#!/usr/bin/env bash
# JM 批量下载 · HTTP 层端到端验收(独立临时数据目录,不触碰现有开发库)
# 覆盖:注册/建库 → 目录候选 → 设置/标签收藏往返 → 建下载任务 → 轮询进度 →
#       zip 落库 → 沙箱清理 → 书库扫描入库 → 自动标签落库(§6.5⑨)
# 上游代理:默认直连(JM_E2E_PROXY=http://... 可覆盖,写入预置 settings.json)
set -u
ROOT="/d/workspace/script/nowen-reader"
TMP="/tmp/nowen-jm-e2e"
PORT=5199
BASE="http://127.0.0.1:5199"
COOKIE="$TMP/cookies.txt"
JM_E2E_PROXY="${JM_E2E_PROXY:-}"

rm -rf "$TMP"; mkdir -p "$TMP/comics" "$TMP/data/jm"
TMP_WIN="$(cygpath -wa "$TMP")"
COMICS_WIN="$(cygpath -wa "$TMP/comics")"

# 预置 JM 设置(旧格式,不含 downloadTags → 同时验证缺省 true 路径)
printf '{"proxy":"%s","imageQuality":"high"}' "$JM_E2E_PROXY" > "$TMP/data/jm/settings.json"

cd "$ROOT" || exit 1
echo "== 构建 =="
go build -o "$TMP/nowen-test.exe" ./cmd/server || exit 1

echo "== 启动服务(独立 DATABASE_URL/DATA_DIR,不触碰开发库) =="
DATA_DIR="$TMP_WIN/data" DATABASE_URL="$TMP_WIN/data/test.db" PORT=$PORT GIN_MODE=release \
  "$TMP/nowen-test.exe" > "$TMP/server.log" 2>&1 &
SRV=$!
trap 'kill $SRV 2>/dev/null' EXIT

for i in $(seq 1 30); do
  curl -s -o /dev/null "$BASE/api/health" && break
  sleep 1
done
if ! curl -s "$BASE/api/health" | grep -q '"status"'; then
  echo "服务未就绪,日志:"; tail -20 "$TMP/server.log"; exit 1
fi

echo "== 注册首个用户(admin) =="
REG=$(curl -s -c "$COOKIE" -X POST "$BASE/api/auth/register" -H 'Content-Type: application/json' \
  -d '{"username":"e2eadmin","password":"e2epass123"}')
echo "$REG" | head -c 200; echo
if ! echo "$REG" | grep -q '"role":"admin"'; then
  echo "注册失败或首个用户非管理员,终止"; exit 1
fi

echo "== 新建漫画书库(书库管理) =="
printf '{"name":"E2E漫画库","type":"comic","rootPath":"%s"}' "${COMICS_WIN//\\/\\\\}" > "$TMP/library.json"
curl -s -b "$COOKIE" -X POST "$BASE/api/admin/libraries" -H 'Content-Type: application/json' \
  --data-binary @"$TMP/library.json" | head -c 300; echo

echo "== GET /api/jm/downloads/dirs =="
curl -s -b "$COOKIE" "$BASE/api/jm/downloads/dirs"; echo

echo "== #27/#28 设置:downloadTags 默认开(§6 自动标签) =="
ST=$(curl -s -b "$COOKIE" "$BASE/api/jm/settings")
echo "$ST" | head -c 300; echo
echo "$ST" | grep -q '"downloadTags":true' || { echo "settings 缺少 downloadTags:true"; exit 1; }

echo "== 标签收藏端点往返(MOBILE_API.md §7 私有扩展) =="
# 注意:中文一律经文件传递(--data-binary @file / --data-urlencode tag@file),
# Windows 下命令行 argv 会经 ANSI 代码页转码导致中文乱码(Git Bash mingw64 curl 实测)。
printf '{"tag":"  E2E测试标签  "}' > "$TMP/tag-payload.json"
printf 'E2E测试标签' > "$TMP/tag-name.txt"
curl -s -b "$COOKIE" -X POST "$BASE/api/jm/tag-favorites" -H 'Content-Type: application/json' \
  --data-binary @"$TMP/tag-payload.json" | head -c 120; echo
curl -s -b "$COOKIE" "$BASE/api/jm/tag-favorites" > "$TMP/tags.json"
python - "$TMP/tags.json" "$TMP/tag-name.txt" <<'PY'
import json,sys
d=json.load(open(sys.argv[1],encoding='utf-8'))
want=open(sys.argv[2],encoding='utf-8').read().strip()
tags=[it['tag'] for it in (d.get('data') or {}).get('list') or []]
assert want in tags, f"标签收藏 POST/GET 失败(应含 trim 后标签): {tags}"
print(f"标签收藏 POST/GET OK(trim 幂等): {tags}")
PY
[ $? -eq 0 ] || exit 1
# tag 值经文件读入、python 做 URL 编码(纯 ASCII 输出,规避 argv 编码与路径转换问题)
ENC=$(python - "$TMP/tag-name.txt" <<'PY'
import sys,urllib.parse
print(urllib.parse.quote(open(sys.argv[1],encoding='utf-8').read().strip()))
PY
)
curl -s -b "$COOKIE" -X DELETE "$BASE/api/jm/tag-favorites?tag=$ENC" | head -c 120; echo
curl -s -b "$COOKIE" "$BASE/api/jm/tag-favorites" > "$TMP/tags.json"
python - "$TMP/tags.json" "$TMP/tag-name.txt" <<'PY'
import json,sys
d=json.load(open(sys.argv[1],encoding='utf-8'))
want=open(sys.argv[2],encoding='utf-8').read().strip()
tags=[it['tag'] for it in (d.get('data') or {}).get('list') or []]
assert want not in tags, f"标签收藏 DELETE 失败(仍存在): {tags}"
print(f"标签收藏 DELETE OK: {tags}")
PY
[ $? -eq 0 ] || exit 1

echo "== 作者类型收藏往返 + 同名共存(type+tag 幂等键) =="
printf '{"tag":"E2E同名人","type":"tag"}' > "$TMP/tag-payload.json"
printf '{"tag":"E2E同名人","type":"author"}' > "$TMP/author-payload.json"
printf 'E2E同名人' > "$TMP/same-name.txt"
curl -s -b "$COOKIE" -X POST "$BASE/api/jm/tag-favorites" -H 'Content-Type: application/json' --data-binary @"$TMP/tag-payload.json" >/dev/null
curl -s -b "$COOKIE" -X POST "$BASE/api/jm/tag-favorites" -H 'Content-Type: application/json' --data-binary @"$TMP/author-payload.json" | head -c 120; echo
curl -s -b "$COOKIE" "$BASE/api/jm/tag-favorites" > "$TMP/tags.json"
python - "$TMP/tags.json" <<'PY'
import json,sys
d=json.load(open(sys.argv[1],encoding='utf-8'))
items=(d.get('data') or {}).get('list') or []
same=[it for it in items if it['tag']=='E2E同名人']
assert len(same)==2 and {it['type'] for it in same}=={'tag','author'}, f"同名双类型应共存: {items}"
print("同名双类型共存 OK:", [(it['type'],it['tag']) for it in same])
PY
[ $? -eq 0 ] || exit 1
ENC2=$(python - "$TMP/same-name.txt" <<'PY'
import sys,urllib.parse
print(urllib.parse.quote(open(sys.argv[1],encoding='utf-8').read().strip()))
PY
)
curl -s -b "$COOKIE" -X DELETE "$BASE/api/jm/tag-favorites?tag=$ENC2&type=author" >/dev/null
curl -s -b "$COOKIE" "$BASE/api/jm/tag-favorites" > "$TMP/tags.json"
python - "$TMP/tags.json" <<'PY'
import json,sys
d=json.load(open(sys.argv[1],encoding='utf-8'))
items=(d.get('data') or {}).get('list') or []
same=[it for it in items if it['tag']=='E2E同名人']
assert len(same)==1 and same[0]['type']=='tag', f"删除 author 型后应剩 tag 型: {items}"
print("按 type 删除互不影响 OK:", [(it['type'],it['tag']) for it in same])
PY
[ $? -eq 0 ] || exit 1
# 清理残留(保持脚本无副作用):tag 型也删掉
curl -s -b "$COOKIE" -X DELETE "$BASE/api/jm/tag-favorites?tag=$ENC2&type=tag" >/dev/null

echo "== 取最新列表前 2 部作为下载目标 =="
curl -s -b "$COOKIE" "$BASE/api/jm/comics/latest?page=1" > "$TMP/latest.json"
python - "$TMP/latest.json" <<'PY'
import json,sys
d=json.load(open(sys.argv[1],encoding='utf-8'))
items=(d.get('data') or {}).get('list') or []
for it in items[:2]:
    print(it['aid'], it['title'][:40])
PY

read -r AID1 TITLE1 < <(python - "$TMP/latest.json" <<'PY'
import json,sys
d=json.load(open(sys.argv[1],encoding='utf-8'))
it=((d.get('data') or {}).get('list') or [])[0]
print(it['aid'], it['title'].replace(' ','_'))
PY
)
read -r AID2 TITLE2 < <(python - "$TMP/latest.json" <<'PY'
import json,sys
d=json.load(open(sys.argv[1],encoding='utf-8'))
items=((d.get('data') or {}).get('list') or [])
it=items[1] if len(items)>1 else items[0]
print(it['aid'], it['title'].replace(' ','_'))
PY
)

DEST="${COMICS_WIN//\\/\\\\}"
echo "== 批量建 2 个任务(验证任务级排队): $AID1 / $AID2 =="
for AID in "$AID1" "$AID2"; do
  curl -s -b "$COOKIE" -X POST "$BASE/api/jm/downloads" -H 'Content-Type: application/json' \
    -d "{\"aid\":\"$AID\",\"title\":\"$AID\",\"destDir\":\"$DEST\"}" | head -c 400; echo
done

echo "== 轮询任务(最多 15 分钟) =="
for i in $(seq 1 180); do
  curl -s -b "$COOKIE" "$BASE/api/jm/downloads" > "$TMP/tasks.json"
  python - "$TMP/tasks.json" <<'PY'
import json,sys
d=json.load(open(sys.argv[1],encoding='utf-8'))
for t in (d.get('data') or {}).get('list') or []:
    print(f"  {t['id']} {t['status']:<8} 章 {sum(1 for c in t['chapters'] if c['state']=='done')}/{len(t['chapters'])} 图 {t['doneImages']}/{t['totalImages']} zip={t.get('zipName') or '-'} err={t.get('error') or ''}")
PY
  DONE=$(python - "$TMP/tasks.json" <<'PY'
import json,sys
d=json.load(open(sys.argv[1],encoding='utf-8'))
ts=(d.get('data') or {}).get('list') or []
print(all(t['status'] in ('done','failed','canceled') for t in ts) and len(ts)>0)
PY
)
  [ "$DONE" = "True" ] && break
  sleep 5
done

echo "== 最终任务状态 =="
python - "$TMP/tasks.json" <<'PY'
import json,sys
d=json.load(open(sys.argv[1],encoding='utf-8'))
for t in (d.get('data') or {}).get('list') or []:
    print(json.dumps({k:t.get(k) for k in ('id','title','status','zipName','zipPath','zipSize','doneImages','totalImages','warning','error')}, ensure_ascii=False, indent=2))
PY

echo "== 目标目录内容 =="
ls -la "$TMP/comics"
echo "== 临时沙箱应不存在 =="
ls -la "$TMP_WIN/data/jm" 2>/dev/null || ls -la "$TMP/data/jm"
echo "== zip 条目抽样(前 5 条) =="
python - "$TMP/comics" <<'PY'
import os,sys,zipfile
root=sys.argv[1]
for f in sorted(os.listdir(root)):
    if f.lower().endswith('.zip'):
        p=os.path.join(root,f)
        with zipfile.ZipFile(p) as z:
            names=z.namelist()
            print(f"{f}  ({len(names)} entries, {os.path.getsize(p)} bytes)")
            for n in names[:5]: print("   ", n)
PY

echo "== 书库扫描入库结果 =="
sleep 3
curl -s -b "$COOKIE" "$BASE/api/comics?page=1&pageSize=10" | head -c 600; echo

echo "== 自动标签:任务快照 tags(§6.5⑨) =="
python - "$TMP/tasks.json" <<'PY'
import json,sys
d=json.load(open(sys.argv[1],encoding='utf-8'))
ts=(d.get('data') or {}).get('list') or []
done=[t for t in ts if t['status']=='done']
ok=False
for t in done:
    tags=t.get('tags') or []
    print(f"  {t.get('title','')[:32]} tags={len(tags)}: {','.join(tags[:5])}{'…' if len(tags)>5 else ''}")
    if tags: ok=True
if not done:
    print("没有 done 状态的任务"); sys.exit(1)
if not ok:
    print("所有任务快照均未捕获 tags"); sys.exit(1)
PY
[ $? -eq 0 ] || exit 1

echo "== 自动标签:轮询 ComicTag 落库(最多 60s) =="
python - "$TMP_WIN/data/test.db" <<'PY'
import sqlite3,sys,time
db=sys.argv[1]
q='''SELECT c."title", t."name" FROM "ComicTag" ct
     JOIN "Comic" c ON c."id"=ct."comicId"
     JOIN "Tag" t ON t."id"=ct."tagId" ORDER BY c."title", t."name"'''
for i in range(20):
    try:
        con=sqlite3.connect(db)
        rows=con.execute(q).fetchall(); con.close()
    except sqlite3.OperationalError as e:
        print(f"  [{i}] db busy: {e}"); time.sleep(3); continue
    if rows:
        print(f"  入库打标 {len(rows)} 条:")
        cur=None
        for title,tag in rows:
            if title!=cur: print(f"  ▸ {title[:40]}"); cur=title
            print(f"     - {tag}")
        sys.exit(0)
    print(f"  [{i}] 尚无 ComicTag,等待…"); time.sleep(3)
print("超时:ComicTag 未落库"); sys.exit(1)
PY
[ $? -eq 0 ] || { echo "== server.log 自动标签行 =="; grep -a "自动标签\|已自动添加" "$TMP/server.log" || true; exit 1; }

echo "== 服务日志尾部 =="
tail -5 "$TMP/server.log"
