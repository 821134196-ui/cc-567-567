const { createApp } = Vue;

const ORDER_STATUS = {
  measuring: '测量中',
  pending_confirm: '待客户确认',
  confirmed: '待加工',
  processed: '已加工/待安装',
  rework_pending: '返工待确认',
  completed: '已完成',
};
const DRAWING_STATUS = { draft: '草稿', pending: '待确认', confirmed: '已确认', superseded: '已作废' };
const PLAN_STATUS = { pending: '待客户确认', approved: '已批准', rejected: '已拒绝', completed: '已完成' };
const ROLES = { surveyor: '测量员', factory: '工厂', installer: '安装员', customer: '客户' };

async function api(method, url, body, isForm) {
  const opts = { method, headers: {} };
  const token = localStorage.getItem('token');
  if (token) opts.headers['Authorization'] = 'Bearer ' + token;
  if (body && !isForm) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  if (isForm) opts.body = body;
  const res = await fetch(url, opts);
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = new Error(data.error || ('请求失败 ' + res.status));
    err.status = res.status;
    throw err;
  }
  return data;
}

// 偏差单元格：超差标红
const DevCells = {
  props: ['dev'],
  computed: {
    items() {
      if (!this.dev) return [];
      return [
        { k: 'X', v: this.dev.posX },
        { k: 'Y', v: this.dev.posY },
        { k: '长', v: this.dev.width },
        { k: '宽', v: this.dev.height },
      ];
    },
  },
  template: `
    <span v-if="!dev" class="muted">—</span>
    <span v-else class="dev-list">
      <span v-for="it in items" :key="it.k" class="dev-item" :class="{bad: Math.abs(it.v) > 2}">
        {{ it.k }}{{ it.v > 0 ? '+' : '' }}{{ Math.abs(it.v) % 1 ? it.v.toFixed(1) : it.v }}
      </span>
    </span>`,
};

createApp({
  components: { DevCells },
  data() {
    return {
      user: JSON.parse(localStorage.getItem('user') || 'null'),
      demoUsers: [],
      loginForm: { username: '', password: '' },
      loginError: '',
      orders: [],
      detail: null,
      tab: 'drawings',
      tabs: [
        { key: 'drawings', label: '图纸版本' },
        { key: 'fabrication', label: '加工记录' },
        { key: 'install', label: '安装复核' },
        { key: 'plans', label: '返工与变更' },
        { key: 'logs', label: '确认与日志' },
      ],
      modal: '',
      toast: '',
      toastType: 'ok',
      activeDrawing: null,
      newOrder: { customerName: '', customerUserId: '', address: '', cutouts: [] },
      newCutout: { kind: 'sink', model: '', posX: 0, posY: 0, width: 0, height: 0 },
      remeasureForm: { cutout: null, model: '', posX: 0, posY: 0, width: 0, height: 0, reason: '' },
      confirmForm: { drawing: null, signature: '' },
      fabForm: { note: '', actuals: [] },
      fieldForm: { cutoutId: null, posX: 0, posY: 0, width: 0, height: 0, note: '' },
      planForm: { cutoutId: null, type: 'rework', description: '', cost: 0, responsibleParty: 'factory' },
    };
  },
  computed: {
    customers() {
      return this.demoUsers.filter((u) => u.role === 'customer');
    },
    draftDrawing() {
      return this.detail && this.detail.drawings.find((d) => d.status === 'draft');
    },
    currentDrawing() {
      if (!this.detail) return null;
      return this.detail.drawings.find((d) => d.id === this.detail.order.currentDrawingId);
    },
    currentVersion() {
      return this.currentDrawing ? this.currentDrawing.version : 0;
    },
    canFieldWork() {
      const s = this.detail.order.status;
      return s === 'processed' || s === 'rework_pending';
    },
    versionWarning() {
      if (!this.detail || !this.detail.fabrication) return '';
      const fabV = this.detail.fabrication.drawingVersion;
      const newer = this.detail.drawings.filter(
        (d) => d.version > fabV && (d.status === 'confirmed' || d.status === 'pending')
      );
      if (!newer.length) return '';
      const vs = newer.map((d) => 'v' + d.version).join('、');
      return `存在更新的图纸版本 ${vs}，但台面已按 v${fabV} 加工，新尺寸不会自动套用。如需按新版施工，请走返工/变更流程。`;
    },
  },
  methods: {
    is(role) { return this.user && this.user.role === role; },
    roleLabel(r) { return ROLES[r] || r; },
    kindLabel(k) { return k === 'sink' ? '水槽' : k === 'cooktop' ? '灶具' : k; },
    orderStatusLabel(s) { return ORDER_STATUS[s] || s; },
    drawingStatusLabel(s) { return DRAWING_STATUS[s] || s; },
    planStatusLabel(s) { return PLAN_STATUS[s] || s; },
    fmtN(n) { return n == null ? '—' : (Math.abs(n) % 1 ? Number(n).toFixed(1) : n); },
    fmtTime(t) { return t ? new Date(t).toLocaleString('zh-CN', { hour12: false }) : '—'; },
    dims(d) { return `${this.fmtN(d.posX)} / ${this.fmtN(d.posY)} / ${this.fmtN(d.width)}×${this.fmtN(d.height)}`; },
    remeasuresOf(cutoutId) {
      return this.detail ? this.detail.remeasures.filter((r) => r.cutoutId === cutoutId) : [];
    },
    showToast(msg, type = 'ok') {
      this.toast = msg;
      this.toastType = type;
      setTimeout(() => { this.toast = ''; }, 3500);
    },
    async run(fn, okMsg) {
      try {
        await fn();
        if (okMsg) this.showToast(okMsg);
      } catch (e) {
        if (e.status === 401) { this.logout(); return; }
        this.showToast(e.message, 'err');
      }
    },

    async loadDemoUsers() {
      try { this.demoUsers = await api('GET', '/api/users'); } catch (e) { /* 忽略 */ }
    },
    async login() {
      this.loginError = '';
      try {
        const data = await api('POST', '/api/login', this.loginForm);
        localStorage.setItem('token', data.token);
        localStorage.setItem('user', JSON.stringify(data.user));
        this.user = data.user;
        await this.loadOrders();
      } catch (e) {
        this.loginError = e.message;
      }
    },
    async logout() {
      try { await api('POST', '/api/logout'); } catch (e) { /* 忽略 */ }
      localStorage.removeItem('token');
      localStorage.removeItem('user');
      this.user = null;
      this.detail = null;
      this.loginForm.password = '';
    },
    async loadOrders() {
      await this.run(async () => { this.orders = await api('GET', '/api/orders'); });
    },
    async openOrder(id) {
      await this.run(async () => {
        this.detail = await api('GET', '/api/orders/' + id);
        this.tab = 'drawings';
      });
    },
    async reload() {
      if (!this.detail) return;
      await this.run(async () => {
        this.detail = await api('GET', '/api/orders/' + this.detail.order.id);
      });
    },
    backToList() {
      this.detail = null;
      this.loadOrders();
    },

    // ---- 测量员 ----
    openNewOrder() {
      this.newOrder = {
        customerName: '', customerUserId: '', address: '',
        cutouts: [
          { kind: 'sink', model: '', posX: 0, posY: 0, width: 0, height: 0 },
          { kind: 'cooktop', model: '', posX: 0, posY: 0, width: 0, height: 0 },
        ],
      };
      this.modal = 'newOrder';
    },
    async createOrder() {
      await this.run(async () => {
        await api('POST', '/api/orders', this.newOrder);
        this.modal = '';
        await this.loadOrders();
      }, '订单已创建');
    },
    async createNewVersion() {
      const note = prompt('换版原因（例如：客户要求水槽右移 20mm）');
      if (note === null) return;
      await this.run(async () => {
        await api('POST', `/api/orders/${this.detail.order.id}/drawings`, { note });
        await this.reload();
      }, '已创建新图纸版本');
    },
    openAddCutout(d) {
      this.activeDrawing = d;
      this.newCutout = { kind: 'sink', model: '', posX: 0, posY: 0, width: 0, height: 0 };
      this.modal = 'addCutout';
    },
    async addCutout() {
      await this.run(async () => {
        await api('POST', `/api/drawings/${this.activeDrawing.id}/cutouts`, this.newCutout);
        this.modal = '';
        await this.reload();
      }, '开孔已添加');
    },
    openRemeasure(ct) {
      this.remeasureForm = {
        cutout: ct, model: ct.model, posX: ct.posX, posY: ct.posY,
        width: ct.width, height: ct.height, reason: '',
      };
      this.modal = 'remeasure';
    },
    async submitRemeasure() {
      const f = this.remeasureForm;
      await this.run(async () => {
        await api('PUT', `/api/cutouts/${f.cutout.id}`, {
          model: f.model, posX: f.posX, posY: f.posY,
          width: f.width, height: f.height, reason: f.reason,
        });
        this.modal = '';
        await this.reload();
      }, '复测数据已保存');
    },
    async removeCutout(ct) {
      if (!confirm('确认删除该开孔？')) return;
      await this.run(async () => {
        await api('DELETE', `/api/cutouts/${ct.id}`);
        await this.reload();
      }, '开孔已删除');
    },
    async uploadImage(ct, ev) {
      const file = ev.target.files[0];
      ev.target.value = '';
      if (!file) return;
      const fd = new FormData();
      fd.append('file', file);
      await this.run(async () => {
        await api('POST', `/api/cutouts/${ct.id}/images`, fd, true);
        await this.reload();
      }, '图片已上传');
    },
    async submitDrawing(d) {
      await this.run(async () => {
        await api('POST', `/api/drawings/${d.id}/submit`);
        await this.reload();
      }, '已提交客户确认');
    },

    // ---- 客户 ----
    openConfirm(d) {
      this.confirmForm = { drawing: d, signature: '' };
      this.modal = 'confirm';
    },
    async confirmDrawing() {
      const d = this.confirmForm.drawing;
      await this.run(async () => {
        const res = await api('POST', `/api/drawings/${d.id}/confirm`, { signature: this.confirmForm.signature });
        this.modal = '';
        await this.reload();
        this.showToast(res.applied ? '已确认，订单进入待加工' : '已确认；台面已加工，新尺寸不自动套用');
      });
    },
    async decidePlan(p, approve) {
      await this.run(async () => {
        await api('POST', `/api/rework-plans/${p.id}/decide`, { approve });
        await this.reload();
      }, approve ? '已批准方案' : '已拒绝方案');
    },

    // ---- 工厂 ----
    openFabricate() {
      const d = this.currentDrawing;
      this.fabForm = {
        note: '',
        actuals: (d.cutouts || []).map((ct) => ({
          cutoutId: ct.id,
          label: this.kindLabel(ct.kind) + ' ' + ct.model,
          posX: ct.posX, posY: ct.posY, width: ct.width, height: ct.height,
        })),
      };
      this.modal = 'fabricate';
    },
    async submitFabricate() {
      await this.run(async () => {
        await api('POST', `/api/orders/${this.detail.order.id}/fabricate`, this.fabForm);
        this.modal = '';
        await this.reload();
      }, '加工完成，版本已锁定');
    },

    // ---- 安装员 ----
    openFieldMeasure() {
      const row = this.detail.comparison.rows[0];
      this.fieldForm = {
        cutoutId: row ? row.cutoutId : null,
        posX: 0, posY: 0, width: 0, height: 0, note: '',
      };
      this.prefillField();
      this.modal = 'fieldMeasure';
    },
    prefillField() {
      const row = this.detail.comparison.rows.find((r) => r.cutoutId === this.fieldForm.cutoutId);
      if (row) {
        this.fieldForm.posX = row.confirmed.posX;
        this.fieldForm.posY = row.confirmed.posY;
        this.fieldForm.width = row.confirmed.width;
        this.fieldForm.height = row.confirmed.height;
      }
    },
    async submitFieldMeasure() {
      await this.run(async () => {
        const res = await api('POST', `/api/orders/${this.detail.order.id}/field-measurements`, this.fieldForm);
        this.modal = '';
        await this.reload();
        this.showToast(res.exceeded ? '已保存：存在超差！' : '已保存：尺寸在公差内', res.exceeded ? 'err' : 'ok');
      });
    },
    openPlan() {
      const bad = this.detail.comparison.rows.find((r) => r.exceeded) || this.detail.comparison.rows[0];
      this.planForm = {
        cutoutId: bad ? bad.cutoutId : null,
        type: 'rework', description: '', cost: 0, responsibleParty: 'factory',
      };
      this.modal = 'plan';
    },
    async submitPlan() {
      await this.run(async () => {
        await api('POST', `/api/orders/${this.detail.order.id}/rework-plans`, this.planForm);
        this.modal = '';
        await this.reload();
      }, '方案已提交，待客户确认');
    },
    async completePlan(p) {
      await this.run(async () => {
        await api('POST', `/api/rework-plans/${p.id}/complete`);
        await this.reload();
      }, '返工已标记完成');
    },
    async completeOrder() {
      if (!confirm('确认完成安装验收？')) return;
      await this.run(async () => {
        await api('POST', `/api/orders/${this.detail.order.id}/complete`);
        await this.reload();
      }, '验收完成，订单关闭');
    },
  },
  mounted() {
    this.loadDemoUsers();
    if (this.user) this.loadOrders();
  },
}).mount('#app');
