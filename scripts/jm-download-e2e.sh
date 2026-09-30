#!/usr/bin/env bash
# JM 批量下载 · HTTP 层端到端验收(独立临时数据目录,不触碰现有开发库)
# 覆盖:注册/建库 → 目录候选 → 建下载任务 → 轮询进度 → zip 落库 → 沙箱清理 → 书库扫描入库
set -u
ROOT="/d/workspace/script/nowen-reader"
TMP="/tmp/nowen-jm-e2e"
PORT=5199
BASE="http://127.0.0.1:5199"
COOKIE="$TMP/cookies.txt"

rm -rf "$TMP"; mkdir -p "$TMP/comics" "$TMP/data"
TMP_WIN="$(cygpath -wa "$TMP")"
COMICS_WIN="$(cygpath -wa "$TMP/comics")"

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
curl -s -b "$COOKIE" -X POST "$BASE/api/admin/libraries" -H 'Content-Type: application/json' \
  -d "{\"name\":\"E2E漫画库\",\"type\":\"comic\",\"rootPath\":\"${COMICS_WIN//\\/\\\\}\"}" | head -c 300; echo

echo "== GET /api/jm/downloads/dirs =="
curl -s -b "$COOKIE" "$BASE/api/jm/downloads/dirs"; echo

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
echo "== 服务日志尾部 =="
tail -5 "$TMP/server.log"
