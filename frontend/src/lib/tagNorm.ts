// 简繁字对表与归一键——与后端 internal/store/tag_norm.go 的 tagNormSimpTradPairs 保持同步，
// frontend/scripts/test-tag-norm.mjs 会在 lint 中对比两份表防止漂移。
// 格式：空白分隔的二字对"简繁"；折叠方向：繁 → 简（简体为规范形）。
// 同一繁体字出现多次时取首个（与后端 buildTagTradToSimp 一致）。
export const TAG_NORM_SIMP_TRAD_PAIRS = `
汉漢 后後 发發 发髮 门門 见見 车車 东東 画畫 图圖 体體 风風 云雲 龙龍 马馬 鱼魚 长長 书書 园園 广廣
庆慶 应應 现現 爱愛 学學 实實 时時 间間 开開 关關 问問 话話 语語 说說 读讀 写寫 记記 让讓 谢謝
请請 讲講 谈談 论論 议議 训訓 设設 访訪 证證 评評 词詞 诗詩 试試 资資 质質 费費 卖賣 买買 贵貴 货貨
财財 贝貝 负負 责責 赛賽 赠贈 赋賦 赢贏 红紅 绿綠 蓝藍 黄黃 银銀 铁鐵 钱錢 组組 织織 细細 线線 练練
结結 绝絕 继繼 续續 级級 纪紀 纯純 约約 经經 绑綁 绳繩 维維 缩縮 网網 轨軌 转轉 轮輪 软軟 轻輕 载載
较較 辅輔 辆輛 轴軸 输輸 针針 钢鋼 钮鈕 铃鈴 铜銅 锁鎖 错錯 键鍵 镜鏡 铺鋪 录錄 闪閃 闭閉 闲閒 闷悶
闹鬧 闻聞 闺閨 阁閣 阅閱 阔闊 队隊 阶階 阳陽 阴陰 陆陸 陈陳 险險 随隨 隐隱 难難 电電 雾霧 飞飛 饭飯
饮飲 饱飽 饲飼 饼餅 馆館 驱驅 驻駐 驾駕 骂罵 骑騎 骗騙 鲜鮮 鸟鳥 鸡雞 鸣鳴 鹰鷹 麦麥 听聽 员員 别別
剂劑 办辦 务務 动動 勋勳 胜勝 势勢 匀勻 医醫 华華 协協 单單 卫衛 卷捲 厂廠 厅廳 历歷 历曆 厉厲 压壓
县縣 参參 双雙 变變 台臺 号號 叹嘆 吓嚇 呜嗚 咏詠 唤喚 启啟 启啓 团團 圆圓 国國 处處 备備 复復 复複
个個 内內 军軍 农農 冲衝 决決 况況 冻凍 净淨 凉涼 减減 凤鳳 凭憑 击擊 划劃 刘劉 则則 刚剛 创創 删刪
剑劍 剥剝 剧劇 劝勸 劳勞 励勵 娱娛 婴嬰 娇嬌 孙孫 宁寧 宝寶 宠寵 审審 对對 寻尋 导導 将將 层層 属屬
屡屢 帅帥 师師 带帶 帮幫 干幹 庄莊 库庫 废廢 弃棄 弹彈 强強 归歸 当當 偿償 忆憶 志誌 怀懷 态態 总總
恋戀 恶惡 悦悅 悬懸 惊驚 惧懼 愿願 战戰 户戶 执執 扩擴 扫掃 扬揚 拥擁 拨撥 择擇 挂掛 挤擠 挥揮 损損
捡撿 换換 舍捨 据據 采採 携攜 摄攝 摆擺 摇搖 撑撐 敌敵 数數 斗鬥 斗鬦 断斷 旧舊 昼晝 显顯 暂暫 畅暢
杀殺 杂雜 权權 条條 来來 极極 构構 枢樞 标標 栏欄 树樹 样樣 桥橋 检檢 楼樓 荣榮 横橫 樱櫻 欢歡 欧歐
残殘 毕畢 气氣 汇匯 没沒 泄洩 泪淚 洁潔 浅淺 测測 济濟 浑渾 浓濃 涂塗 涛濤 涨漲 渐漸 渔漁 渗滲 温溫
湿濕 满滿 滤濾 滥濫 滨濱 滚滾 滞滯 灭滅 潜潛 濒瀕 灯燈 灵靈 灾災 灿燦 为為 炼煉 炽熾 烂爛 烟煙 烦煩
烧燒 热熱 状狀 犹猶 独獨 狮獅 猎獵 猪豬 献獻 环環 玛瑪 疗療 稳穩 穷窮 系係 系繫 竞競 节節 笔筆 笼籠
篮籃 简簡 签簽 类類 紧緊 联聯 职職 脑腦 脚腳 脸臉 兴興 术術 众眾 众衆 订訂 讯訊 询詢 该該 详詳 夸誇
诚誠 误誤 谁誰 谐諧 诺諾 谣謠 识識 丰豐 郑鄭 虚虛 虽雖 虾蝦 蚀蝕 蚂螞 袜襪 装裝 里裏 里裡 观觀 页頁
顶頂 顺順 须須 头頭 频頻 颗顆 题題 颜顏 顾顧 齐齊 龟龜 鲁魯 胡鬍 静靜 严嚴 优優 伞傘 伟偉 传傳 伤傷
伦倫 侦偵 侧側 侨僑 俩倆 俭儉 债債 倾傾 储儲 儿兒 亿億 从從 义義 乌烏 乐樂 乔喬 习習 乱亂 争爭 亏虧
产產 亩畝 亲親 价價 临臨 举舉 蛮蠻 万萬 与與 几幾 只隻 码碼 宫宮 丽麗 梦夢 声聲 药藥 兽獸 触觸 猫貓
丝絲 萝蘿 戏戲 游遊 制製 周週 面麵
`;

function buildTradToSimp(): Map<string, string> {
  const m = new Map<string, string>();
  for (const pair of TAG_NORM_SIMP_TRAD_PAIRS.split(/\s+/)) {
    if (!pair) continue;
    const chars = Array.from(pair);
    if (chars.length !== 2 || chars[0] === chars[1]) continue;
    if (!m.has(chars[1])) m.set(chars[1], chars[0]);
  }
  return m;
}

const tagTradToSimp = buildTradToSimp();

function computeTagNormKey(name: string): string {
  const lower = name.trim().toLowerCase();
  if (!lower) return "";
  let out = "";
  for (const ch of lower) {
    out += tagTradToSimp.get(ch) ?? ch;
  }
  return out;
}

// 与后端 TagNormKey 同口径：TrimSpace → 小写 → 繁→简折叠。仅用于匹配，不改变展示名。
// 结果按入参缓存（标签名是有限集合）：键入时每次只需一次归一 + n 次缓存键的子串匹配。
const normKeyCache = new Map<string, string>();

export function tagNormKey(name: string): string {
  let key = normKeyCache.get(name);
  if (key === undefined) {
    key = computeTagNormKey(name);
    if (normKeyCache.size < 50000) normKeyCache.set(name, key);
  }
  return key;
}

// 标签名是否命中搜索词：归一键子串匹配（繁简互通；空词视为全匹配）。
export function tagMatchesQuery(name: string, query: string): boolean {
  const q = tagNormKey(query.trim());
  if (!q) return true;
  return tagNormKey(name).includes(q);
}
