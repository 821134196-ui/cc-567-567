#!/usr/bin/env bash
# 版本与权限规则核对脚本
# 用法: ./verify.sh [BASE_URL]
# 覆盖：1) 尺寸超差  2) 客户尚未确认  3) 加工后换版  4) 返工确认链
#       以及跨岗位越权 403、未登录 401、复测前后值保留、图片本地存储
set -u
BASE="${1:-http://localhost:8080}"
PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf "  \033[32mPASS\033[0m %s\n" "$1"; }
bad() { FAIL=$((FAIL+1)); printf "  \033[31mFAIL\033[0m %s\n   ↳ %s\n" "$1" "$2"; }

# api METHOD PATH TOKEN [JSON]  -> body 写入 /tmp/ct_body，HTTP 码写入 /tmp/ct_code
api() {
  local method="$1" path="$2" tok="$3" data="${4:-}"
  if [ -n "$data" ]; then
    curl -s -o /tmp/ct_body -w "%{http_code}" -X "$method" "$BASE$path" \
      -H "Authorization: Bearer $tok" -H "Content-Type: application/json" -d "$data" > /tmp/ct_code
  else
    curl -s -o /tmp/ct_body -w "%{http_code}" -X "$method" "$BASE$path" \
      -H "Authorization: Bearer $tok" > /tmp/ct_code
  fi
}
http_code() { cat /tmp/ct_code; }
resp() { cat /tmp/ct_body; }
# check <名称> <期望码> [响应中应包含的字符串]
check() {
  local code; code=$(http_code)
  if [ "$2" = "$code" ] && { [ -z "${3:-}" ] || grep -qF "$3" /tmp/ct_body; }; then
    ok "$1 (HTTP $code)"
  else
    bad "$1" "期望 $2 实际 $code；响应: $(head -c 220 /tmp/ct_body)"
  fi
}
# jget <a.b.c> —— 从 /tmp/ct_body 读取，PascalCase/snake_case 均兼容
jget() {
  python3 - "$1" <<'PY'
import sys, json
key = sys.argv[1]
d = json.load(open("/tmp/ct_body"))
def norm(k): return k.lower().replace("_", "")
def find(obj, name):
    if isinstance(obj, dict):
        for k, v in obj.items():
            if norm(k) == name: return v
        return None
    return None
cur = d
for part in key.split("."):
    if isinstance(cur, list):
        cur = cur[int(part)]
    else:
        cur = find(cur, norm(part))
if isinstance(cur, (dict, list)):
    print(json.dumps(cur, ensure_ascii=False))
elif cur is not None:
    print(cur)
PY
}

login() {
  curl -s -X POST "$BASE/api/login" -H "Content-Type: application/json" \
    -d "{\"username\":\"$1\",\"password\":\"$2\"}" \
  | python3 -c "import sys,json;print(json.load(sys.stdin).get('token',''))"
}

echo "== 登录（四种岗位）=="
SUR=$(login surveyor sur12345); FAC=$(login factory fac12345)
INS=$(login installer ins12345); CUS=$(login customer cus12345)
for t in "测量员:$SUR" "工厂:$FAC" "安装人员:$INS" "客户:$CUS"; do
  [ -n "${t#*:}" ] && ok "${t%%:*}登录" || bad "${t%%:*}登录" "无token"
done

api GET /api/orders ""; check "未登录访问被拒" 401 "未登录"

api GET /api/orders "$SUR"
oid() { jget "list" >/dev/null 2>&1; python3 - "$1" <<'PY'
import sys, json
code = sys.argv[1]
d = json.load(open("/tmp/ct_body"))
print(next(o["ID"] for o in d if o["Code"] == code))
PY
}
O1=$(oid O-DEV-01); O2=$(oid O-NEW-02); O3=$(oid O-VER-03); O4=$(oid O-RWK-04)
echo "  工单ID: 超差=$O1 未确认=$O2 加工后换版=$O3 返工=$O4"

echo; echo "== 权限隔离：岗位只能做自己获准的操作 =="
api POST "/api/orders/$O1/production/start" "$INS" '{}'; check "安装人员不能开工" 403 "无权操作"
api POST "/api/orders/$O1/site-measurements" "$FAC" '{"slot":"sink","width":760,"depth":450}'
check "工厂不能做现场测量" 403 "无权操作"
api POST "/api/orders/$O2/versions" "$FAC" '{"openings":[]}'; check "工厂不能出图纸版本" 403 "无权操作"
api POST "/api/orders/$O1/costs" "$CUS" '{"amount":100,"responsible_party":"factory"}'
check "客户不能登记费用" 403 "无权操作"
api POST "/api/orders/$O1/accept" "$INS" '{}'; check "安装人员不能验收" 403 "无权操作"
api GET "/api/orders/$O1" "$CUS"; check "客户可查看工单" 200 ""

echo; echo "== 例2：客户尚未确认 → 工厂不能加工 =="
api POST "/api/orders/$O2/production/start" "$FAC" '{}'
check "未确认图纸禁止开工" 409 "客户尚未签字确认"
api GET "/api/orders/$O2" "$SUR"; VID2=$(jget "versions.0.id")
api POST "/api/versions/$VID2/confirm" "$SUR" '{}'; check "测量员不能替客户确认" 403 "无权操作"

echo; echo "== 复测：同一开孔保留前后数值及原因 =="
api POST "/api/orders/$O2/openings/sink/remeasure" "$SUR" \
  '{"width":762,"depth":451,"pos_x":100,"pos_y":90,"reason":"核对脚本复测：台面进场前再次复尺","note":"自动化核对","images":[]}'
check "提交复测" 201 ""
api GET "/api/orders/$O2" "$SUR"
python3 - <<'PY'
import json
d = json.load(open("/tmp/ct_body"))
rs = [m for m in d["measures"] if m["Stage"] == "remeasure"]
last = rs[-1] if rs else {}
ok = last.get("PrevWidth") == 760 and last.get("Width") == 762 and bool(last.get("Reason"))
print("  ", "\033[32mPASS\033[0m" if ok else "\033[31mFAIL\033[0m",
      "复测前后值 前760→后762 且原因保留")
import sys; sys.exit(0 if ok else 1)
PY
f=$?; FAIL=$((FAIL + f)); PASS=$((PASS + 1 - f))

echo; echo "== 客户确认例2后工厂可开工（版本流转） =="
api POST "/api/versions/$VID2/confirm" "$CUS" '{"note":"核对脚本客户签字"}'
check "客户签字确认 V1" 200 "confirmed"
api POST "/api/orders/$O2/production/start" "$FAC" '{}'; check "确认后工厂可开工" 201 ""
api POST "/api/orders/$O2/versions" "$SUR" \
  '{"note":"加工中换版","openings":[{"slot":"sink","kind":"水槽","model":"x","width":1,"depth":1}]}'
check "加工中禁止换版" 409 "已加工"

echo; echo "== 例3：加工后换版必须被拦截（已加工台面不自动套用新尺寸） =="
api POST "/api/orders/$O3/versions" "$SUR" \
  '{"note":"V2 灶具改700x400","submit":true,"openings":[{"slot":"cooktop","kind":"灶具","model":"美的 Q310","pos_x":90,"pos_y":540,"width":700,"depth":400,"tolerance":2}]}'
check "已加工台面换版被拒绝" 409 "不能自动套用"
api GET "/api/orders/$O3" "$SUR"
python3 - <<'PY'
import json
d = json.load(open("/tmp/ct_body"))
# V2 不应落库，仍只有 V1
nos = sorted(v["VersionNo"] for v in d["versions"])
ok = nos == [1]
print("  ", "\033[32mPASS\033[0m" if ok else "\033[31mFAIL\033[0m",
      "被拒后未产生新版本（版本号=%s），加工基线仍为 V1" % nos)
import sys; sys.exit(0 if ok else 1)
PY
f=$?; FAIL=$((FAIL + f)); PASS=$((PASS + 1 - f))

echo; echo "== 例1：尺寸超差 → 未确认禁止返工，闭环前禁止安装 =="
api POST "/api/orders/$O1/install" "$INS" '{}'; check "偏差未闭环禁止安装" 409 "不能安装"
api GET "/api/orders/$O1" "$INS"; D1=$(jget "deviations.0.id")
api POST "/api/deviations/$D1/propose" "$FAC" '{"plan":"直接现场扩孔"}'
check "工厂不能提返工方案" 403 "无权操作"
api POST "/api/deviations/$D1/propose" "$INS" \
  '{"type":"rework","plan":"返厂补衬条修边恢复760±2，严禁现场改孔","estimated_cost":350,"responsible_party":"factory"}'
check "安装人员提返工方案" 201 ""
api GET "/api/orders/$O1" "$INS"; RW1=$(jget "deviations.0.rework_events.0.id")
api POST "/api/reworks/$RW1/execute" "$FAC" '{"result_note":"先斩后奏"}'
check "客户未确认禁止工厂返工" 409 "客户尚未确认"
api POST "/api/reworks/$RW1/execute" "$INS" '{}'; check "安装人员不能执行返工" 403 "无权操作"
api POST "/api/reworks/$RW1/confirm" "$SUR" '{}'; check "测量员不能替客户确认返工" 403 "无权操作"
api POST "/api/reworks/$RW1/confirm" "$CUS" '{}'; check "客户确认返工方案" 200 "customer_confirmed"
api POST "/api/reworks/$RW1/execute" "$FAC" '{"result_note":"补条修边完成"}'
check "确认后工厂执行返工" 200 "done"
api POST "/api/orders/$O1/costs" "$FAC" \
  "{\"amount\":350,\"category\":\"rework\",\"responsible_party\":\"factory\",\"note\":\"返工工时+材料\",\"rework_event_id\":$RW1}"
check "费用责任方单独记录" 201 "350"
api POST "/api/deviations/$D1/resolve" "$INS" '{"note":"返工后复测760合格","width":760,"depth":450}'
check "返工后复核闭环（安装人员）" 200 "resolved"
api POST "/api/deviations/$D1/resolve" "$FAC" '{}'; check "工厂不能代替复核闭环" 403 "无权操作"
api POST "/api/orders/$O1/install" "$INS" '{"note":"复装"}'; check "闭环后允许安装" 201 ""
api POST "/api/orders/$O1/accept" "$INS" '{}'; check "仅客户可验收（安装人员被拒）" 403 "无权操作"
api POST "/api/orders/$O1/accept" "$CUS" '{"note":"验收通过"}'; check "客户验收" 201 ""

echo; echo "== 例4：完整返工链留痕可查（版本/确认/返工/费用） =="
api GET "/api/orders/$O4" "$CUS"
python3 - <<'PY'
import json
d = json.load(open("/tmp/ct_body"))
checks = {
  "有客户图纸确认记录": any(c["Action"] == "confirm" for c in d["confirmations"]),
  "返工方案经客户确认后执行": any(
      r["Status"] == "done" and r["CustomerConfirmedByName"] and r["ExecutedByName"]
      for dev in d["deviations"] for r in dev["ReworkEvents"]),
  "费用与责任方独立记录(¥300/工厂)": any(
      c["Amount"] == 300 and c["ResponsibleParty"] == "factory" for c in d["costs"]),
  "加工版本锁定 V1": d["ProducedVersionID"] and d["ProducedVersionID"] == d["CurrentVersionID"],
  "含返工后复测记录": any(m["Stage"] == "post_rework" for m in d["measures"]),
  "时间线含客户验收": any(t["type"] == "accept" for t in d["timeline"]),
  "工单状态=已验收": d["Status"] == "accepted",
}
fails = 0
for k, v in checks.items():
    print("  ", "\033[32mPASS\033[0m" if v else "\033[31mFAIL\033[0m", k)
    fails += not v
import sys; sys.exit(fails)
PY
f=$?; FAIL=$((FAIL + f)); PASS=$((PASS + 7 - f))

echo; echo "== 图片上传保存在本地 =="
python3 -c "open('/tmp/t.png','wb').write(bytes.fromhex('89504e470d0a1a0a'))"
code=$(curl -s -o /tmp/ct_body -w "%{http_code}" -X POST "$BASE/api/uploads" \
  -H "Authorization: Bearer $SUR" -F "image=@/tmp/t.png;type=image/png")
echo "$code" > /tmp/ct_code
check "上传返回本地URL" 201 "/uploads/"
URL=$(python3 -c "import json;print(json.load(open('/tmp/ct_body'))['url'])")
curl -s -o /dev/null "$BASE$URL" && ok "图片可通过本地路径访问 ($URL)"

echo
echo "================ 结果: $PASS 通过, $FAIL 失败 ================"
exit $FAIL
