const { createApp, ref, reactive, computed, onMounted } = Vue;

const API = "/api";

const STATUS_MAP = {
  measuring: ["测量中", "gray"], pending_confirm: ["待客户确认", "amber"],
  confirmed: ["图纸已确认", "blue"], producing: ["加工中", "violet"],
  produced: ["已加工/待安装", "blue"], blocked: ["偏差处理中", "red"],
  ready_install: ["可安装", "green"], installed: ["已安装/待验收", "amber"],
  accepted: ["已验收", "green"],
};
const VER_MAP = {
  draft: ["草稿", "gray"], submitted: ["待客户确认", "amber"],
  confirmed: ["客户已确认", "green"], superseded: ["已被替代", "gray"],
  rejected: ["客户驳回", "red"],
};
const DEV_MAP = {
  open: ["待提方案", "red"], proposal_pending: ["方案待客户确认", "amber"],
  customer_confirmed: ["客户已确认/待返工", "blue"], rework_done: ["返工完成/待复核", "violet"],
  resolved: ["已闭环", "green"], rejected: ["方案被驳回", "red"],
};
const RW_MAP = {
  proposed: ["待客户确认", "amber"], customer_confirmed: ["客户已确认", "blue"],
  rejected: ["已驳回", "red"], done: ["已执行", "green"],
};
const STAGE_MAP = {
  initial: "初测", remeasure: "复测", factory_actual: "工厂实际开孔",
  site: "现场测量", post_rework: "返工后复测",
};
const PARTY_MAP = { factory: "工厂", surveyor: "测量员", installer: "安装人员", customer: "客户" };

createApp({
  setup() {
    const view = ref("login");
    const user = ref(null);
    const token = ref(localStorage.getItem("ct_token") || "");
    const orders = ref([]);
    const order = ref(null);
    const flash = ref("");
    const flashType = ref("ok");
    const loading = ref(false);

    const loginForm = reactive({ username: "", password: "" });

    function showFlash(msg, type = "ok") {
      flash.value = msg; flashType.value = type;
      setTimeout(() => (flash.value = ""), 4000);
    }

    async function api(path, opts = {}) {
      const headers = { Authorization: "Bearer " + token.value, ...(opts.headers || {}) };
      if (opts.body && !(opts.body instanceof FormData)) {
        headers["Content-Type"] = "application/json";
        opts.body = JSON.stringify(opts.body);
      }
      const res = await fetch(API + path, { ...opts, headers });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error || "请求失败 (" + res.status + ")");
      return snakeKeys(data);
    }

    async function upload(file) {
      const fd = new FormData();
      fd.append("image", file);
      return api("/uploads", { method: "POST", body: fd });
    }

    async function login(username, password) {
      try {
        const r = await fetch(API + "/login", {
          method: "POST", headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ username, password }),
        });
        const data = await r.json();
        if (!r.ok) throw new Error(data.error);
        token.value = data.token; user.value = data.user;
        localStorage.setItem("ct_token", data.token);
        await loadOrders();
        view.value = "list";
      } catch (e) { showFlash(e.message, "err"); }
    }

    async function doLogin() { await login(loginForm.username, loginForm.password); }

    async function restoreSession() {
      if (!token.value) return;
      try {
        user.value = await api("/me");
        await loadOrders();
        view.value = "list";
      } catch (e) { logout(); }
    }

    function logout() {
      token.value = ""; user.value = null;
      localStorage.removeItem("ct_token");
      view.value = "login";
    }

    // Go 结构体未加 json tag 的字段按 PascalCase 返回，这里统一转成 snake_case；
    // 连续大写缩写（ID/URL 等）整体转小写；已是 snake_case 的键保持不变
    function snakeKeys(obj) {
      if (Array.isArray(obj)) return obj.map(snakeKeys);
      if (obj && typeof obj === "object") {
        const out = {};
        for (const [k, v] of Object.entries(obj)) {
          const s = k
            .replace(/([a-z0-9])([A-Z])/g, "$1_$2")
            .replace(/([A-Z]+)([A-Z][a-z])/g, "$1_$2")
            .toLowerCase();
          out[s] = snakeKeys(v);
        }
        return out;
      }
      return obj;
    }

    async function loadOrders() {
      orders.value = await api("/orders");
    }

    async function openOrder(id) {
      loading.value = true;
      try { order.value = await api("/orders/" + id); view.value = "detail"; }
      catch (e) { showFlash(e.message, "err"); }
      loading.value = false;
    }

    function back() { loadOrders(); view.value = "list"; }

    async function refreshOrder() {
      order.value = await api("/orders/" + order.value.id);
    }

    const role = computed(() => user.value?.role);
    const is = (...roles) => roles.includes(role.value);

    function badge(map, key) {
      return map[key] || [key, "gray"];
    }
    function fmt(t) {
      if (!t) return "";
      return new Date(t).toLocaleString("zh-CN", { hour12: false });
    }
    function imagesOf(m) {
      try { return JSON.parse(m.images || "[]"); } catch { return []; }
    }
    function signed(delta, tol) {
      return (delta > 0 ? "+" : "") + Number(delta).toFixed(1);
    }
    function deltaClass(delta, tol) {
      return Math.abs(delta) > tol ? "delta-bad" : "delta-ok";
    }

    // ---------- 三列对比：确认尺寸 | 工厂实际开孔 | 现场测量 ----------
    const compareRows = computed(() => {
      const o = order.value;
      if (!o) return [];
      const baseVer = o.produced_version_id || o.current_version_id;
      const ver = (o.versions || []).find(v => v.id === baseVer);
      const latest = (slot, stage) => {
        const list = (o.measures || []).filter(m => m.slot === slot && m.stage === stage);
        return list.length ? list[list.length - 1] : null;
      };
      const rows = [];
      (ver?.openings || []).forEach(op => {
        const f = latest(op.slot, "factory_actual");
        const s = latest(op.slot, "site");
        const d = (o.deviations || []).filter(x => x.slot === op.slot && x.exceeded);
        const activeDev = d.length ? d[d.length - 1] : null;
        rows.push({ op, factory: f, site: s, dev: activeDev });
      });
      return rows;
    });

    const openDeviations = computed(() =>
      (order.value?.deviations || []).filter(d => d.exceeded && d.status !== "resolved"));

    // ---------- 表单 ----------
    const newOrder = reactive({ code: "", customer_name: "", address: "", note: "" });
    async function createOrder() {
      try {
        await api("/orders", { method: "POST", body: { ...newOrder } });
        showFlash("工单已创建");
        newOrder.code = newOrder.customer_name = newOrder.address = newOrder.note = "";
        await loadOrders();
      } catch (e) { showFlash(e.message, "err"); }
    }

    const newVersion = reactive({
      note: "", submit: true,
      sink: { model: "欧琳 OL-7843", pos_x: 120, pos_y: 80, width: 760, depth: 450, tolerance: 2 },
      cooktop: { model: "方太 TH26B", pos_x: 100, pos_y: 560, width: 680, depth: 380, tolerance: 2 },
      useSink: true, useCooktop: true,
    });
    async function createVersion() {
      const openings = [];
      if (newVersion.useSink) openings.push({ slot: "sink", kind: "水槽", ...newVersion.sink });
      if (newVersion.useCooktop) openings.push({ slot: "cooktop", kind: "灶具", ...newVersion.cooktop });
      try {
        await api(`/orders/${order.value.id}/versions`, {
          method: "POST", body: { note: newVersion.note, submit: newVersion.submit, openings },
        });
        showFlash(newVersion.submit ? "新版图纸已提交客户确认" : "草稿已保存");
        newVersion.note = "";
        await refreshOrder();
      } catch (e) { showFlash(e.message, "err"); }
    }

    async function confirmVersion(vid, note = "") {
      try { await api(`/versions/${vid}/confirm`, { method: "POST", body: { note } });
        showFlash("已签字确认，工厂可按此版本加工"); await refreshOrder();
      } catch (e) { showFlash(e.message, "err"); }
    }
    async function rejectVersion(vid) {
      const note = prompt("请填写驳回意见（将通知测量员重新出图）", "尺寸需要调整");
      if (note === null) return;
      try { await api(`/versions/${vid}/reject`, { method: "POST", body: { note } });
        showFlash("已驳回"); await refreshOrder();
      } catch (e) { showFlash(e.message, "err"); }
    }

    const remeasure = reactive({ slot: "sink", width: 760, depth: 450, pos_x: 120, pos_y: 80,
      reason: "", note: "", files: [], urls: [] });
    async function remeasureFiles(e) {
      remeasure.files = Array.from(e.target.files);
      remeasure.urls = [];
      for (const f of remeasure.files) {
        const r = await upload(f); remeasure.urls.push(r.url);
      }
    }
    async function submitRemeasure() {
      try {
        await api(`/orders/${order.value.id}/openings/${remeasure.slot}/remeasure`, {
          method: "POST",
          body: { width: +remeasure.width, depth: +remeasure.depth, pos_x: +remeasure.pos_x,
            pos_y: +remeasure.pos_y, reason: remeasure.reason, note: remeasure.note,
            images: remeasure.urls },
        });
        showFlash("复测已记录（前后数值与原因已保留）");
        remeasure.reason = remeasure.note = ""; remeasure.urls = []; remeasure.files = [];
        await refreshOrder();
      } catch (e) { showFlash(e.message, "err"); }
    }

    async function startProduction() {
      try { await api(`/orders/${order.value.id}/production/start`, { method: "POST", body: {} });
        showFlash("已开工（锁定客户确认版本）"); await refreshOrder();
      } catch (e) { showFlash(e.message, "err"); }
    }
    const finishForm = reactive({ useActuals: false, sinkW: 760, sinkD: 450, note: "加工完成" });
    async function finishProduction() {
      const body = { actuals: [], note: finishForm.note };
      if (finishForm.useActuals) {
        body.actuals.push({ slot: "sink", width: +finishForm.sinkW, depth: +finishForm.sinkD,
          pos_x: 120, pos_y: 80 });
      }
      try { await api(`/orders/${order.value.id}/production/finish`, { method: "POST", body });
        showFlash("加工完成，实际开孔已登记，加工版本已锁定"); await refreshOrder();
      } catch (e) { showFlash(e.message, "err"); }
    }

    const siteForm = reactive({ slot: "sink", width: 765, depth: 450, pos_x: 120, pos_y: 80,
      note: "", urls: [] });
    async function siteFiles(e) {
      for (const f of Array.from(e.target.files)) {
        const r = await upload(f); siteForm.urls.push(r.url);
      }
    }
    async function submitSite() {
      try {
        const r = await api(`/orders/${order.value.id}/site-measurements`, {
          method: "POST",
          body: { slot: siteForm.slot, width: +siteForm.width, depth: +siteForm.depth,
            pos_x: +siteForm.pos_x, pos_y: +siteForm.pos_y, note: siteForm.note,
            images: siteForm.urls },
        });
        siteForm.note = ""; siteForm.urls = [];
        showFlash(r.deviation.exceeded ? "现场测量已登记：判定超差，工单已锁定待处理！" :
          "现场测量已登记：在公差内", r.deviation.exceeded ? "err" : "ok");
        await refreshOrder();
      } catch (e) { showFlash(e.message, "err"); }
    }

    const proposal = reactive({ did: 0, type: "rework", plan: "", estimated_cost: 300,
      responsible_party: "factory" });
    function openProposal(did) { proposal.did = did; proposal.plan = ""; }
    async function submitProposal() {
      try {
        await api(`/deviations/${proposal.did}/propose`, { method: "POST", body: { ...proposal } });
        showFlash("返工/变更方案已提交，等待客户确认"); proposal.did = 0;
        await refreshOrder();
      } catch (e) { showFlash(e.message, "err"); }
    }
    async function confirmRework(rid) {
      try { await api(`/reworks/${rid}/confirm`, { method: "POST", body: {} });
        showFlash("已确认方案，工厂可以执行返工"); await refreshOrder();
      } catch (e) { showFlash(e.message, "err"); }
    }
    async function rejectRework(rid) {
      const reason = prompt("驳回原因", "不接受该方案");
      if (reason === null) return;
      try { await api(`/reworks/${rid}/reject`, { method: "POST", body: { reason } });
        showFlash("方案已驳回"); await refreshOrder();
      } catch (e) { showFlash(e.message, "err"); }
    }
    async function executeRework(rid) {
      const note = prompt("返工执行结果说明", "已按方案修孔，尺寸恢复公差内");
      if (note === null) return;
      try { await api(`/reworks/${rid}/execute`, { method: "POST", body: { result_note: note } });
        showFlash("返工已执行，等待安装方复核"); await refreshOrder();
      } catch (e) { showFlash(e.message, "err"); }
    }
    async function resolveDeviation(did) {
      const note = prompt("返工后复核说明", "返工后复测合格，偏差闭环");
      if (note === null) return;
      try { await api(`/deviations/${did}/resolve`, { method: "POST",
        body: { note, width: 0 } });
        showFlash("偏差已闭环"); await refreshOrder();
      } catch (e) { showFlash(e.message, "err"); }
    }

    const costForm = reactive({ amount: 300, category: "rework", responsible_party: "factory", note: "" });
    async function addCost() {
      try { await api(`/orders/${order.value.id}/costs`, { method: "POST", body: { ...costForm } });
        showFlash("费用与责任方已单独记录"); costForm.note = ""; await refreshOrder();
      } catch (e) { showFlash(e.message, "err"); }
    }

    async function installOrder() {
      const note = prompt("安装备注", "现场安装完成");
      if (note === null) return;
      try { await api(`/orders/${order.value.id}/install`, { method: "POST", body: { note } });
        showFlash("已登记安装，等待客户验收"); await refreshOrder();
      } catch (e) { showFlash(e.message, "err"); }
    }
    async function acceptOrder() {
      const note = prompt("验收意见（签字）", "试装合格，同意验收");
      if (note === null) return;
      try { await api(`/orders/${order.value.id}/accept`, { method: "POST", body: { note } });
        showFlash("验收完成"); await refreshOrder();
      } catch (e) { showFlash(e.message, "err"); }
    }

    onMounted(restoreSession);

    return {
      view, user, orders, order, flash, flashType, loading, loginForm, newOrder,
      newVersion, remeasure, finishForm, siteForm, proposal, costForm,
      doLogin, login, logout, loadOrders, openOrder, back, refreshOrder,
      role, is, badge, fmt, imagesOf, signed, deltaClass,
      STATUS_MAP, VER_MAP, DEV_MAP, RW_MAP, STAGE_MAP, PARTY_MAP, compareRows, openDeviations,
      createOrder, createVersion, confirmVersion, rejectVersion,
      remeasureFiles, submitRemeasure, startProduction, finishProduction,
      siteFiles, submitSite, openProposal, submitProposal,
      confirmRework, rejectRework, executeRework, resolveDeviation,
      addCost, installOrder, acceptOrder,
    };
  },
  template: `
  <div>
    <!-- 登录 -->
    <div v-if="view==='login'" class="wrap">
      <div class="card login-box">
        <h2>厨房台面开孔复核与返工系统</h2>
        <div v-if="flash" class="flash" :class="flashType">{{ flash }}</div>
        <label>用户名</label>
        <input v-model="loginForm.username" style="width:100%" placeholder="surveyor / factory / installer / customer">
        <label>密码</label>
        <input v-model="loginForm.password" type="password" style="width:100%" @keyup.enter="doLogin">
        <div style="margin-top:12px"><button class="btn primary" @click="doLogin">登录</button></div>
        <div class="demo">
          <button class="btn sm" @click="login('surveyor','sur12345')">测量员 / sur12345</button>
          <button class="btn sm" @click="login('factory','fac12345')">工厂 / fac12345</button>
          <button class="btn sm" @click="login('installer','ins12345')">安装人员 / ins12345</button>
          <button class="btn sm" @click="login('customer','cus12345')">客户 / cus12345</button>
        </div>
      </div>
    </div>

    <template v-else>
    <header class="topbar">
      <h1>厨房台面开孔复核与返工系统</h1>
      <div class="who">
        <span class="pill-role">{{ {surveyor:'测量员',factory:'工厂',installer:'安装人员',customer:'客户'}[user.role] }}</span>
        {{ user.name }}
        <button class="btn sm" @click="logout">退出</button>
      </div>
    </header>

    <!-- 工单列表 -->
    <div v-if="view==='list'" class="wrap">
      <div v-if="flash" class="flash" :class="flashType">{{ flash }}</div>
      <div class="cards">
        <div class="card" style="grid-column:1/-1">
          <h2>工单列表（{{ orders.length }}）</h2>
          <div v-for="o in orders" :key="o.id" class="order-item" @click="openOrder(o.id)">
            <div>
              <span class="code">{{ o.code }}</span>
              <span class="muted small"> · {{ o.customer_name }} · {{ o.address }}</span>
              <div class="small muted" style="margin-top:2px">{{ o.note }}</div>
            </div>
            <span class="badge" :class="badge(STATUS_MAP,o.status)[1]">{{ badge(STATUS_MAP,o.status)[0] }}</span>
          </div>
        </div>

        <div v-if="is('surveyor')" class="card">
          <h2>新建工单（测量员）</h2>
          <label>工单号</label><input v-model="newOrder.code" placeholder="O-XXX-NN">
          <label>客户姓名</label><input v-model="newOrder.customer_name">
          <label>安装地址</label><input v-model="newOrder.address">
          <label>备注</label><input v-model="newOrder.note">
          <div style="margin-top:10px"><button class="btn primary" @click="createOrder">创建工单</button></div>
        </div>
      </div>
    </div>

    <!-- 工单详情 -->
    <div v-if="view==='detail' && order" class="wrap">
      <span class="back" @click="back">← 返回工单列表</span>
      <div v-if="flash" class="flash" :class="flashType">{{ flash }}</div>

      <div class="card">
        <h2>
          {{ order.code }}
          <span class="badge" :class="badge(STATUS_MAP,order.status)[1]">{{ badge(STATUS_MAP,order.status)[0] }}</span>
        </h2>
        <div class="muted small">{{ order.customer_name }} · {{ order.address }} · {{ order.note }}</div>
        <div class="small muted" style="margin-top:4px">
          当前图纸版本：V{{ (order.versions.find(v=>v.id===order.current_version_id)||{}).version_no || '—' }}；
          实际加工版本：<b>{{ order.produced_version_id ? 'V'+(order.versions.find(v=>v.id===order.produced_version_id)||{}).version_no : '尚未加工' }}</b>
        </div>
        <div class="section-actions">
          <button v-if="is('factory')" class="btn primary sm"
            :disabled="order.status!=='confirmed'" @click="startProduction">按确认版本开工</button>
          <button v-if="is('installer') && (order.status==='produced'||order.status==='ready_install')"
            class="btn ok sm" @click="installOrder">登记安装完成</button>
          <button v-if="is('customer') && order.status==='installed'"
            class="btn ok sm" @click="acceptOrder">客户签字验收</button>
        </div>
        <div v-if="is('factory') && order.status!=='confirmed'" class="small muted" style="margin-top:6px">
          仅“图纸已确认”状态可开工；客户尚未确认时按钮禁用。
        </div>
      </div>

      <!-- 三列对比 -->
      <div class="card">
        <h2>安装前三方对比：确认尺寸 ｜ 实际开孔 ｜ 现场测量</h2>
        <div class="compare-wrap">
        <table class="threecol">
          <thead>
            <tr>
              <th>开孔</th>
              <th class="col-confirm">型号</th>
              <th class="col-confirm">确认宽×深(mm)</th>
              <th class="col-confirm">确认定位 X/Y</th>
              <th class="col-factory">工厂实际宽×深</th>
              <th class="col-factory">差值</th>
              <th class="col-site">现场测量宽×深</th>
              <th class="col-site">对确认差值(±公差)</th>
              <th>状态</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in compareRows" :key="r.op.id">
              <td>{{ r.op.kind }}（{{ r.op.slot==='sink'?'水槽':'灶具' }}）</td>
              <td class="col-confirm">{{ r.op.model }}</td>
              <td class="col-confirm mono">{{ r.op.width }}×{{ r.op.depth }}</td>
              <td class="col-confirm mono">{{ r.op.pos_x }}/{{ r.op.pos_y }}</td>
              <td class="col-factory mono">
                <span v-if="r.factory">{{ r.factory.width }}×{{ r.factory.depth }}</span>
                <span v-else class="muted">未登记</span>
              </td>
              <td class="col-factory mono" v-if="r.factory">
                <span :class="deltaClass(r.factory.width-r.op.width,r.op.tolerance)">{{ signed(r.factory.width-r.op.width) }}</span>
                /<span :class="deltaClass(r.factory.depth-r.op.depth,r.op.tolerance)">{{ signed(r.factory.depth-r.op.depth) }}</span>
              </td>
              <td class="col-factory" v-else>—</td>
              <td class="col-site mono">
                <span v-if="r.site">{{ r.site.width }}×{{ r.site.depth }}</span>
                <span v-else class="muted">未测量</span>
              </td>
              <td class="col-site mono" v-if="r.site">
                <span :class="deltaClass(r.site.width-r.op.width,r.op.tolerance)">{{ signed(r.site.width-r.op.width) }}</span>
                /<span :class="deltaClass(r.site.depth-r.op.depth,r.op.tolerance)">{{ signed(r.site.depth-r.op.depth) }}</span>
                <div class="small muted">公差 ±{{ r.op.tolerance }}</div>
              </td>
              <td class="col-site" v-else>—</td>
              <td>
                <span v-if="r.dev" class="badge red">超差处理中</span>
                <span v-else-if="r.site" class="badge green">合格</span>
                <span v-else class="badge gray">待现场测量</span>
              </td>
            </tr>
          </tbody>
        </table>
        </div>
      </div>

      <div class="cards">
        <div>
          <!-- 图纸版本 -->
          <div class="card">
            <h2>图纸版本与客户确认</h2>
            <div v-for="v in order.versions" :key="v.id" style="border-bottom:1px solid var(--line);padding:8px 0">
              <div style="display:flex;justify-content:space-between;align-items:center">
                <b>V{{ v.version_no }}</b>
                <span class="badge" :class="badge(VER_MAP,v.status)[1]">{{ badge(VER_MAP,v.status)[0] }}</span>
              </div>
              <div class="small muted">{{ v.note }} · {{ v.created_by_name }} · {{ fmt(v.created_at) }}</div>
              <table style="margin-top:6px">
                <tr v-for="op in v.openings" :key="op.id">
                  <td>{{ op.kind }}</td><td>{{ op.model }}</td>
                  <td class="mono">{{ op.width }}×{{ op.depth }}mm</td>
                  <td class="muted small">定位 {{ op.pos_x }}/{{ op.pos_y }} · 公差±{{ op.tolerance }}</td>
                </tr>
              </table>
              <div class="section-actions">
                <button v-if="is('customer') && v.status==='submitted'" class="btn ok sm"
                  @click="confirmVersion(v.id)">签字确认此版本</button>
                <button v-if="is('customer') && v.status==='submitted'" class="btn danger sm"
                  @click="rejectVersion(v.id)">驳回</button>
                <span v-if="v.status==='confirmed'" class="small muted">
                  确认人：{{ v.confirmed_by_name }} · {{ fmt(v.confirmed_at) }}
                </span>
              </div>
            </div>

            <div v-if="is('surveyor')" class="form-inline">
              <h3>出新版图纸 / 换版（测量员）</h3>
              <div v-if="order.produced_version_id" class="flash err">
                该台面已按 V{{ (order.versions.find(x=>x.id===order.produced_version_id)||{}).version_no }} 加工完成：
                新尺寸<b>不会自动套用</b>到已加工台面，请安装方提偏差后走返工/变更流程。
              </div>
              <label>版本说明</label><input v-model="newVersion.note" placeholder="如：客户更换灶具型号">
              <div style="margin-top:8px">
                <label><input type="checkbox" v-model="newVersion.useSink"> 水槽</label>
                <div class="row" v-if="newVersion.useSink">
                  <div><label>型号</label><input v-model="newVersion.sink.model"></div>
                  <div><label>宽</label><input type="number" v-model.number="newVersion.sink.width"></div>
                  <div><label>深</label><input type="number" v-model.number="newVersion.sink.depth"></div>
                  <div><label>X</label><input type="number" v-model.number="newVersion.sink.pos_x"></div>
                  <div><label>Y</label><input type="number" v-model.number="newVersion.sink.pos_y"></div>
                </div>
                <label style="margin-top:6px"><input type="checkbox" v-model="newVersion.useCooktop"> 灶具</label>
                <div class="row" v-if="newVersion.useCooktop">
                  <div><label>型号</label><input v-model="newVersion.cooktop.model"></div>
                  <div><label>宽</label><input type="number" v-model.number="newVersion.cooktop.width"></div>
                  <div><label>深</label><input type="number" v-model.number="newVersion.cooktop.depth"></div>
                  <div><label>X</label><input type="number" v-model.number="newVersion.cooktop.pos_x"></div>
                  <div><label>Y</label><input type="number" v-model.number="newVersion.cooktop.pos_y"></div>
                </div>
              </div>
              <label style="margin-top:8px"><input type="checkbox" v-model="newVersion.submit"> 创建后直接提交客户签字</label>
              <div style="margin-top:8px">
                <button class="btn primary sm" @click="createVersion"
                  :disabled="!!order.produced_version_id || order.status==='producing'">
                  创建新版本
                </button>
                <span v-if="order.produced_version_id || order.status==='producing'" class="small muted">已加工/加工中，换版按钮锁定</span>
              </div>
            </div>
          </div>

          <!-- 测量记录（含复测前后值） -->
          <div class="card">
            <h2>测量记录与复测历史</h2>
            <table>
              <thead><tr><th>时间</th><th>开孔</th><th>阶段</th><th>宽×深</th>
                <th>复测前→后</th><th>原因/备注</th><th>图片</th><th>测量人</th></tr></thead>
              <tbody>
                <tr v-for="m in order.measures" :key="m.id">
                  <td class="small">{{ fmt(m.created_at) }}</td>
                  <td>{{ m.slot==='sink'?'水槽':'灶具' }}</td>
                  <td>{{ STAGE_MAP[m.stage] }}</td>
                  <td class="mono">{{ m.width }}×{{ m.depth }}</td>
                  <td class="small">
                    <span v-if="m.stage==='remeasure'" class="mono">
                      {{ m.prev_width }}×{{ m.prev_depth }}<br>→ {{ m.width }}×{{ m.depth }}
                    </span>
                    <span v-else class="muted">—</span>
                  </td>
                  <td class="small" style="white-space:normal;max-width:220px">
                    <span v-if="m.reason">原因：{{ m.reason }}<br></span>{{ m.note }}
                  </td>
                  <td>
                    <a v-for="(img,i) in imagesOf(m)" :key="i" :href="img" target="_blank">
                      <img :src="img" class="thumb">
                    </a>
                  </td>
                  <td class="small">{{ m.measured_by_name }}</td>
                </tr>
              </tbody>
            </table>

            <div v-if="is('surveyor')" class="form-inline">
              <h3>同一开孔复测（保留前后数值与原因）</h3>
              <div class="row">
                <div><label>开孔</label>
                  <select v-model="remeasure.slot"><option value="sink">水槽</option><option value="cooktop">灶具</option></select>
                </div>
                <div><label>宽</label><input type="number" v-model.number="remeasure.width"></div>
                <div><label>深</label><input type="number" v-model.number="remeasure.depth"></div>
                <div><label>X</label><input type="number" v-model.number="remeasure.pos_x"></div>
                <div><label>Y</label><input type="number" v-model.number="remeasure.pos_y"></div>
              </div>
              <label>复测原因（必填）</label><input v-model="remeasure.reason" placeholder="如：客户更换型号 / 现场条件变化">
              <label>备注</label><input v-model="remeasure.note">
              <label>测量图片</label><input type="file" accept="image/*" multiple @change="remeasureFiles">
              <div style="margin-top:8px"><button class="btn primary sm" @click="submitRemeasure">提交复测</button></div>
            </div>
          </div>
        </div>

        <div>
          <!-- 偏差与返工 -->
          <div class="card">
            <h2>开孔偏差与返工 / 变更</h2>
            <div v-if="!order.deviations.length" class="small muted">暂无现场偏差记录。</div>
            <div v-for="d in order.deviations.filter(x=>x.exceeded)" :key="d.id"
              style="border:1px solid var(--line);border-radius:8px;padding:10px;margin-bottom:10px">
              <div style="display:flex;justify-content:space-between">
                <b>{{ d.kind }}开孔超差（{{ d.slot==='sink'?'水槽':'灶具' }}）</b>
                <span class="badge" :class="badge(DEV_MAP,d.status)[1]">{{ badge(DEV_MAP,d.status)[0] }}</span>
              </div>
              <table style="margin:6px 0">
                <tr><th>项</th><th>确认尺寸</th><th>现场实测</th><th>差值</th></tr>
                <tr><td>宽</td><td class="mono">{{ d.expect_width }}</td><td class="mono">{{ d.actual_width }}</td>
                  <td class="mono" :class="deltaClass(d.delta_width,d.tolerance)">{{ signed(d.delta_width) }}</td></tr>
                <tr><td>深</td><td class="mono">{{ d.expect_depth }}</td><td class="mono">{{ d.actual_depth }}</td>
                  <td class="mono" :class="deltaClass(d.delta_depth,d.tolerance)">{{ signed(d.delta_depth) }}</td></tr>
              </table>
              <div class="small muted">{{ d.note }} · 发现人 {{ d.found_by_name }} · {{ fmt(d.created_at) }}（公差 ±{{ d.tolerance }}mm）</div>

              <div v-for="rw in d.rework_events" :key="rw.id" class="form-inline" style="margin-top:8px">
                <div style="display:flex;justify-content:space-between">
                  <b>{{ rw.type==='change'?'变更方案':'返工方案' }}</b>
                  <span class="badge" :class="badge(RW_MAP,rw.status)[1]">{{ badge(RW_MAP,rw.status)[0] }}</span>
                </div>
                <div class="small" style="margin:4px 0;white-space:normal">{{ rw.plan }}</div>
                <div class="small muted">
                  预估费用 ¥{{ rw.estimated_cost }} · 责任方：{{ PARTY_MAP[rw.responsible_party] }} ·
                  提出人 {{ rw.proposed_by_name }}
                  <span v-if="rw.confirmed_at">· 客户 {{ rw.customer_confirmed_by_name }} 于 {{ fmt(rw.confirmed_at) }} 确认</span>
                  <span v-if="rw.executed_at">· {{ rw.executed_by_name }} 于 {{ fmt(rw.executed_at) }} 执行：{{ rw.result_note }}</span>
                  <span v-if="rw.rejected_reason">· 驳回原因：{{ rw.rejected_reason }}</span>
                </div>
                <div class="section-actions">
                  <button v-if="is('customer') && rw.status==='proposed'" class="btn ok sm"
                    @click="confirmRework(rw.id)">客户确认方案</button>
                  <button v-if="is('customer') && rw.status==='proposed'" class="btn danger sm"
                    @click="rejectRework(rw.id)">驳回方案</button>
                  <button v-if="is('factory') && rw.status==='customer_confirmed'" class="btn primary sm"
                    @click="executeRework(rw.id)">工厂执行返工</button>
                </div>
                <div v-if="is('factory') && rw.status==='proposed'" class="small muted" style="margin-top:4px">
                  客户尚未确认，工厂不能执行（严禁现场未经同意改孔）。
                </div>
              </div>

              <div class="section-actions">
                <button v-if="is('installer') && (d.status==='open'||d.status==='rejected')"
                  class="btn warn sm" @click="openProposal(d.id)">提出返工/变更方案</button>
                <button v-if="is('installer') && d.status==='rework_done'"
                  class="btn ok sm" @click="resolveDeviation(d.id)">返工后复核闭环</button>
              </div>

              <div v-if="proposal.did===d.id" class="form-inline">
                <div class="row">
                  <div><label>类型</label>
                    <select v-model="proposal.type"><option value="rework">返工</option><option value="change">变更</option></select>
                  </div>
                  <div><label>预估费用(元)</label><input type="number" v-model.number="proposal.estimated_cost"></div>
                  <div><label>责任方</label>
                    <select v-model="proposal.responsible_party">
                      <option value="factory">工厂</option><option value="surveyor">测量员</option>
                      <option value="installer">安装人员</option><option value="customer">客户</option>
                    </select>
                  </div>
                </div>
                <label>处理方案（提交后需客户确认）</label>
                <textarea v-model="proposal.plan" rows="3" style="width:100%"
                  placeholder="如：返厂补衬条修边，恢复 680±2mm；不得现场直接扩孔"></textarea>
                <div style="margin-top:8px">
                  <button class="btn primary sm" @click="submitProposal">提交方案给客户</button>
                </div>
              </div>
            </div>

            <div v-if="is('installer') && (order.status==='produced'||order.status==='blocked'||order.status==='ready_install')" class="form-inline">
              <h3>安装现场测量（安装人员）</h3>
              <div class="row">
                <div><label>开孔</label>
                  <select v-model="siteForm.slot"><option value="sink">水槽</option><option value="cooktop">灶具</option></select>
                </div>
                <div><label>实测宽</label><input type="number" v-model.number="siteForm.width"></div>
                <div><label>实测深</label><input type="number" v-model.number="siteForm.depth"></div>
                <div><label>X</label><input type="number" v-model.number="siteForm.pos_x"></div>
                <div><label>Y</label><input type="number" v-model.number="siteForm.pos_y"></div>
              </div>
              <label>现场情况</label><input v-model="siteForm.note">
              <label>现场照片</label><input type="file" accept="image/*" multiple @change="siteFiles">
              <div style="margin-top:8px"><button class="btn primary sm" @click="submitSite">提交并自动判定超差</button></div>
            </div>
          </div>

          <!-- 加工 -->
          <div class="card" v-if="is('factory')">
            <h2>工厂加工</h2>
            <div v-if="order.productions.length">
              <table>
                <tr v-for="p in order.productions" :key="p.id">
                  <td>依据 V{{ p.version_no }}</td>
                  <td><span class="badge" :class="p.status==='done'?'green':'violet'">
                    {{ p.status==='done'?'加工完成':'加工中' }}</span></td>
                  <td class="small">{{ p.operator_name }}</td>
                  <td class="small">{{ p.finished_at?fmt(p.finished_at):fmt(p.started_at) }}</td>
                </tr>
              </table>
            </div>
            <div v-if="order.status==='producing'" class="form-inline">
              <h3>登记实际开孔并完工</h3>
              <label><input type="checkbox" v-model="finishForm.useActuals"> 水槽实际尺寸与图纸不同</label>
              <div class="row" v-if="finishForm.useActuals">
                <div><label>实际宽</label><input type="number" v-model.number="finishForm.sinkW"></div>
                <div><label>实际深</label><input type="number" v-model.number="finishForm.sinkD"></div>
              </div>
              <label>完工备注</label><input v-model="finishForm.note">
              <div style="margin-top:8px"><button class="btn ok sm" @click="finishProduction">完成加工</button></div>
            </div>
            <div v-else-if="order.status!=='produced' && !order.produced_version_id" class="small muted">
              客户确认图纸后，使用页面顶部“按确认版本开工”。
            </div>
          </div>

          <!-- 费用 -->
          <div class="card">
            <h2>费用与责任方（单独记录）</h2>
            <table>
              <tr v-for="c in order.costs" :key="c.id">
                <td class="mono">¥{{ c.amount }}</td>
                <td>{{ {rework:'返工',change:'变更',material:'材料'}[c.category]||c.category }}</td>
                <td>责任方：{{ PARTY_MAP[c.responsible_party] }}</td>
                <td class="small muted">{{ c.note }}</td>
                <td class="small muted">{{ c.created_by_name }}</td>
              </tr>
              <tr v-if="!order.costs.length"><td class="muted small">暂无费用记录</td></tr>
            </table>
            <div v-if="!is('customer')" class="form-inline">
              <div class="row">
                <div><label>金额</label><input type="number" v-model.number="costForm.amount"></div>
                <div><label>类别</label>
                  <select v-model="costForm.category">
                    <option value="rework">返工</option><option value="change">变更</option><option value="material">材料</option>
                  </select>
                </div>
                <div><label>责任方</label>
                  <select v-model="costForm.responsible_party">
                    <option value="factory">工厂</option><option value="surveyor">测量员</option>
                    <option value="installer">安装人员</option><option value="customer">客户</option>
                  </select>
                </div>
              </div>
              <label>说明</label><input v-model="costForm.note">
              <div style="margin-top:8px"><button class="btn primary sm" @click="addCost">登记费用</button></div>
            </div>
          </div>

          <!-- 时间线 -->
          <div class="card">
            <h2>全程留痕（客户确认 / 图纸版本 / 返工记录）</h2>
            <ul class="timeline">
              <li v-for="(t,i) in order.timeline" :key="i">
                <div class="t-title">{{ t.title }}</div>
                <div class="t-detail" v-if="t.detail">{{ t.detail }}</div>
                <div class="t-meta">{{ fmt(t.at) }} · {{ t.actor }}</div>
              </li>
            </ul>
          </div>
        </div>
      </div>
    </div>
    </template>
  </div>
  `,
}).mount("#app");
