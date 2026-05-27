import "./styles.css";

type Item = {
  id: number;
  name: string;
  category: string;
  owner: string;
  brand: string;
  spec: string;
  unit: string;
  barcode: string;
  minStock: number;
  note: string;
  totalStock: number;
  totalCost: number;
  isLowStock: boolean;
};

type Location = {
  id: number;
  name: string;
  description: string;
  photoPath: string;
};

type Batch = {
  id: number;
  itemId: number;
  locationId: number;
  locationName: string;
  locationPhoto: string;
  photoPath: string;
  initialQuantity: number;
  currentQuantity: number;
  cost: number;
  status: string;
  note: string;
};

type Movement = {
  id: number;
  itemId: number;
  userName: string;
  type: string;
  quantity: number;
  note: string;
  createdAt: string;
};

type User = {
  id: number;
  name: string;
  username: string;
  phone: string;
  role: "admin" | "member";
  status: "active" | "disabled";
  createdAt: string;
};

type BarcodeDetectorCtor = new (options?: { formats?: string[] }) => {
  detect(video: HTMLVideoElement): Promise<Array<{ rawValue: string }>>;
};

type BarcodeImageReader = {
  decodeFromImageUrl(url: string): Promise<{ getText(): string }>;
};

type ThemeName = "warm" | "classic";
type ViewName = "home" | "items" | "locations" | "scan" | "settings" | "accounts";

let imageBarcodeReaderPromise: Promise<BarcodeImageReader> | null = null;

const defaultUnits = ["包", "只", "支", "件", "瓶", "片"];
const owners = ["男主人用", "女主人用", "公共使用", "宠物专用"];
const app = document.querySelector<HTMLDivElement>("#app")!;
let items: Item[] = [];
let locations: Location[] = [];
let users: User[] = [];
let currentUser: User | null = null;
let selectedItem: number | null = null;
let showItemForm = false;
let showItemEditForm = false;
let showLocationForm = false;
let editingLocationID: number | null = null;
let activeView: ViewName = "home";
let pendingRequests = 0;
let currentTheme: ThemeName = readTheme();
let suppressHistoryUpdate = false;

type AppHistoryState = {
  view: ViewName;
  itemId?: number | null;
};

async function api<T>(url: string, options: RequestInit = {}): Promise<T> {
  beginNetwork();
  try {
    const headers: HeadersInit = options.body instanceof FormData ? {} : { "Content-Type": "application/json" };
    const res = await fetch(url, { credentials: "same-origin", ...options, headers: { ...headers, ...options.headers } });
    if (res.status === 401) {
      renderLogin();
      throw new Error("unauthorized");
    }
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || "请求失败");
    return data as T;
  } finally {
    endNetwork();
  }
}

window.addEventListener("unhandledrejection", (event) => {
  const reason = event.reason;
  showError(reason instanceof Error ? reason.message : "操作失败，请稍后重试。");
});

async function boot() {
  applyTheme(currentTheme);
  if ("serviceWorker" in navigator) navigator.serviceWorker.register("/sw.js").catch(() => undefined);
  try {
    currentUser = await api<User>("/api/me");
    if (currentUser.role === "admin") {
      activeView = "accounts";
      await loadUsers();
    } else {
      activeView = "home";
      await loadData();
    }
    replaceHistoryState();
    render();
  } catch {
    renderLogin();
  }
}

async function loadData() {
  const [nextItems, nextLocations] = await Promise.all([api<Item[] | null>("/api/items"), api<Location[] | null>("/api/locations")]);
  items = Array.isArray(nextItems) ? nextItems : [];
  locations = Array.isArray(nextLocations) ? nextLocations : [];
}

async function loadUsers() {
  const nextUsers = await api<User[] | null>("/api/users");
  users = Array.isArray(nextUsers) ? nextUsers : [];
}

function renderLogin() {
  app.innerHTML = `
    <main class="login">
      <section class="login-panel">
        <p class="eyebrow">Household Inventory</p>
        <h1>家用库存</h1>
        <form id="loginForm" class="stack">
          <label>手机号 / 管理员账号<input name="login" autocomplete="username" inputmode="tel" /></label>
          <label>密码<input name="password" type="password" autocomplete="current-password" /></label>
          <button class="primary">登录</button>
        </form>
      </section>
    </main>`;
  form("loginForm", async (fd) => {
    const login = String(fd.get("login") || "").trim();
    if (!isValidLogin(login)) throw new Error("请输入 admin 或 11 位手机号");
    currentUser = await api<User>("/api/login", { method: "POST", body: JSON.stringify(toObject(fd)) });
    if (currentUser.role === "admin") {
      activeView = "accounts";
      await loadUsers();
    } else {
      activeView = "home";
      await loadData();
    }
    render();
  });
}

function render() {
  const isAdmin = currentUser?.role === "admin";
  app.innerHTML = `
    <div class="shell">
      <header class="topbar">
        <div><p class="eyebrow">家用库存</p><h1>${title()}</h1></div>
        <button id="refreshBtn" class="icon-btn" title="刷新">↻</button>
      </header>
      <nav class="tabs ${isAdmin ? "admin-tabs" : ""}">
        ${
          isAdmin
            ? `${tab("accounts", "账号")}`
            : `${tab("home", "首页")}
        ${tab("items", "物品")}
        ${tab("locations", "地点")}
        ${tab("scan", "查找")}
        ${tab("settings", "设置")}`
        }
      </nav>
      <main class="content">${view()}</main>
    </div>`;
  bindGlobal();
  bindForms();
}

function title() {
  return { home: "库存总览", items: "物品管理", locations: "存放地点", scan: "快速查找", settings: "设置", accounts: "账号管理" }[activeView];
}

function tab(view: ViewName, label: string) {
  return `<button data-view="${view}" class="${activeView === view ? "active" : ""}">${label}</button>`;
}

function view() {
  if (currentUser?.role === "admin") return accountsView();
  if (activeView === "items" && selectedItem) return itemDetailPlaceholder();
  if (activeView === "items") return itemsView();
  if (activeView === "locations") return locationsView();
  if (activeView === "scan") return scanView();
  if (activeView === "settings") return settingsView();
  return homeView();
}

function homeView() {
  const low = items.filter((item) => item.isLowStock);
  return `
    <section class="metric-strip">
      <button data-view="items"><strong>${items.length}</strong><span>物品</span></button>
      <button data-view="locations"><strong>${locations.length}</strong><span>地点</span></button>
      <button data-scroll-low-stock><strong>${low.length}</strong><span>待补货</span></button>
    </section>
    <section id="lowStockBand" class="band">
      <h2>待补货</h2>
      <div class="list">${low.length ? low.map(itemCard).join("") : empty("暂时没有低库存物品")}</div>
    </section>
    <section class="band">
      <h2>最近物品</h2>
      <div class="list">${items.slice(0, 6).map(itemCard).join("") || empty("先添加一个物品")}</div>
    </section>`;
}

function itemsView() {
  return `
    <section class="toolbar-band">
      <button id="toggleItemFormBtn" class="primary">${showItemForm ? "收起" : "添加"}</button>
    </section>
    ${
      showItemForm
        ? `<section class="band">
      <form id="itemForm" class="labeled-form">
        ${itemFormFields()}
        <button class="primary full-submit">新增物品</button>
      </form>
    </section>`
        : ""
    }
    <section class="band"><div class="list">${items.map(itemCard).join("") || empty("暂无物品")}</div></section>`;
}

function locationsView() {
  return `
    <section class="toolbar-band">
      <button id="toggleLocationFormBtn" class="primary">${showLocationForm ? "收起" : "添加"}</button>
    </section>
    ${
      showLocationForm
        ? `<section class="band">
      <form id="locationForm" class="grid-form">
        <input name="name" placeholder="地点名称，例如 储物间左柜" required />
        <textarea name="description" placeholder="位置描述"></textarea>
        <button class="primary">新增地点</button>
      </form>
    </section>`
        : ""
    }
    <section class="location-grid">${locations.map(locationCard).join("") || empty("暂无地点")}</section>`;
}

function scanView() {
  return `
    <section class="band">
      <form id="searchForm" class="stack">
        <label>条码或关键词
          <div class="input-action">
            <input id="quickSearchInput" name="query" placeholder="输入条码、物品名称、品牌或规格" required />
            <button type="button" id="scanSearchBarcodeBtn">扫码</button>
          </div>
        </label>
        <button class="primary">查找物品</button>
      </form>
      <div id="searchResult" class="result"></div>
    </section>`;
}

function settingsView() {
  return `
    <section class="band prose">
      <h2>主题风格</h2>
      <p>可以随时在温馨版和经典版之间切换，只会保存在当前设备上。</p>
      <div class="theme-options">
        <button class="theme-choice ${currentTheme === "warm" ? "active" : ""}" data-theme-choice="warm" type="button">
          <strong>温馨之家</strong>
          <span>奶油色、暖木色、柔和卡片</span>
        </button>
        <button class="theme-choice ${currentTheme === "classic" ? "active" : ""}" data-theme-choice="classic" type="button">
          <strong>经典清爽</strong>
          <span>米白底、深绿强调、信息更利落</span>
        </button>
      </div>
    </section>
    <section class="band prose">
      <h2>首版设置</h2>
      <p>当前版本面向家庭自用。普通成员没有账号资料修改权限，如需调整手机号、用户名或密码，请让 admin 账号处理。</p>
      <p>数据保存在 SQLite 文件和 data/uploads 目录。部署前建议定期备份这两处。</p>
      <button id="logoutBtn" class="danger">退出登录</button>
    </section>`;
}

function accountsView() {
  return `
    <section class="band prose">
      <h2>家庭成员</h2>
      <p>admin 只负责账号管理，不能进入库存管理页面。普通成员使用手机号和密码登录库存系统。</p>
      <form id="memberForm" class="labeled-form">
        ${fieldRow("手机号", `<input name="phone" inputmode="tel" maxlength="11" placeholder="11 位手机号" required />`)}
        ${fieldRow("用户名", `<input name="username" placeholder="例如：老婆、老公、妈妈" required />`)}
        ${fieldRow("密码", `<input name="password" type="password" autocomplete="new-password" required />`)}
        ${fieldRow("状态", statusSelect("active"))}
        <div class="form-actions"><button class="primary">新增成员</button></div>
      </form>
    </section>
    <section class="band">
      <h2>账号列表</h2>
      <div class="list">${users.map(userCard).join("") || empty("暂无账号")}</div>
    </section>
    <section class="band">
      <button id="logoutBtn" class="danger">退出登录</button>
    </section>`;
}

function userCard(user: User) {
  if (user.role === "admin") {
    return `
      <article class="row-card">
        <div><strong>admin</strong><span>管理员账号 · 密码只能通过 INVENTORY_ADMIN_PASSWORD 更新</span></div>
        <div class="status-pill">管理员</div>
      </article>`;
  }
  return `
    <article class="row-card user-card">
      <form class="userEditForm" data-user="${user.id}">
        <div class="user-form-grid">
          <label>手机号<input name="phone" inputmode="tel" maxlength="11" value="${escapeHtml(user.phone)}" required /></label>
          <label>用户名<input name="username" value="${escapeHtml(user.username)}" required /></label>
          <label>新密码<input name="password" type="password" placeholder="不修改可留空" autocomplete="new-password" /></label>
          <label>状态${statusSelect(user.status)}</label>
        </div>
        <div class="row-actions">
          <span class="status-pill ${user.status === "disabled" ? "muted" : ""}">${user.status === "active" ? "启用中" : "已停用"}</span>
          <button class="small primary">保存</button>
        </div>
      </form>
    </article>`;
}

function statusSelect(value: User["status"]) {
  return `<select name="status"><option value="active" ${value === "active" ? "selected" : ""}>启用</option><option value="disabled" ${value === "disabled" ? "selected" : ""}>停用</option></select>`;
}

function itemCard(item: Item) {
  const titleMeta = [item.brand, item.spec].filter(Boolean).join(" · ");
  const meta = [item.owner, item.category].filter(Boolean).join(" · ");
  return `
    <article class="row-card" data-item="${item.id}">
      <div>
        <strong>${escapeHtml(item.name)}${titleMeta ? `<span class="title-meta">${escapeHtml(titleMeta)}</span>` : ""}</strong>
        <span>${escapeHtml(meta)}${item.totalCost > 0 ? `${meta ? " · " : ""}总开销 ${fmtMoney(item.totalCost)}` : ""}</span>
      </div>
      <div class="row-actions">
        <div class="stock ${item.isLowStock ? "low" : ""}">${fmt(item.totalStock)} ${escapeHtml(item.unit)}</div>
        <button class="small" data-edit-item="${item.id}" title="编辑物品">编辑</button>
        <button class="danger small" data-delete-item="${item.id}" title="删除物品">删除</button>
      </div>
    </article>`;
}

function locationCard(loc: Location) {
  const isEditing = editingLocationID === loc.id;
  return `
    <article class="location-card">
      <div class="photo location-hero">
        ${loc.photoPath ? `<img src="${loc.photoPath}" alt="${escapeHtml(loc.name)}" />` : ""}
        <div class="location-mask">
          <strong>${escapeHtml(loc.name)}</strong>
          <p>${escapeHtml(loc.description || "暂无描述")}</p>
        </div>
        <form class="photoForm" data-location="${loc.id}">
          <input id="photo-${loc.id}" name="photo" class="photo-input" type="file" accept="image/*" capture="environment" />
          <label class="photo-float-button" for="photo-${loc.id}">${loc.photoPath ? "换照片" : "上传照片"}</label>
        </form>
      </div>
      <div class="location-actions">
        <button class="small" data-edit-location="${loc.id}">${isEditing ? "收起" : "编辑"}</button>
      </div>
      ${
        isEditing
          ? `<form class="locationEditForm location-edit-form" data-location="${loc.id}">
        <input name="name" value="${escapeHtml(loc.name)}" required />
        <textarea name="description">${escapeHtml(loc.description || "")}</textarea>
        <div class="form-actions">
          <button class="primary small">保存</button>
          <button type="button" class="small" data-cancel-location-edit>取消</button>
        </div>
      </form>`
          : ""
      }
    </article>`;
}

function itemDetailPlaceholder() {
  return `<section id="detail" class="band">${empty("加载中")}</section>`;
}

async function renderItemDetail(id: number) {
  const data = await api<{ item: Item; batches: Batch[]; movements: Movement[] }>(`/api/items/${id}`);
  const item = data.item;
  const itemMeta = [item.owner, item.brand, item.spec, item.category].filter(Boolean);
  const canConsume = data.batches.some((batch) => batch.status !== "done" && batch.currentQuantity > 0);
  app.querySelector(".content")!.innerHTML = `
    <section class="detail-head">
      <button id="backBtn">← 返回</button>
      <div class="detail-title">
        <h2>${escapeHtml(item.name)}</h2>
        <p>${itemMeta.length ? escapeHtml(itemMeta.join(" · ")) : "暂无品牌、规格或分类"}</p>
        ${item.totalCost > 0 ? `<p class="cost-line">总开销 ${fmtMoney(item.totalCost)}</p>` : ""}
        <div class="stock ${item.isLowStock ? "low" : ""}">${fmt(item.totalStock)} ${escapeHtml(item.unit)}</div>
      </div>
      <button id="toggleEditItemBtn">${showItemEditForm ? "收起编辑" : "编辑物品"}</button>
    </section>
    ${
      showItemEditForm
        ? `<section class="band">
      <h2>编辑物品</h2>
      <form id="editItemForm" class="labeled-form">
        ${itemFormFields(item)}
        <div class="form-actions"><button class="primary">保存修改</button></div>
      </form>
    </section>`
        : ""
    }
    <section class="band">
      <h2>入库</h2>
      <form id="batchForm" class="labeled-form">
        ${fieldRow("存放地点", locationSelect())}
        ${fieldRow("数量", `<input name="initialQuantity" type="number" min="0.01" step="0.01" required />`)}
        ${fieldRow("花费", `<div class="input-action"><input name="cost" type="number" min="0" step="0.01" value="0" /><span class="currency-suffix">人民币</span></div>`)}
        ${fieldRow("位置照片", `<input name="photo" type="file" accept="image/*" capture="environment" /><p class="field-hint">可选：拍下这个批次具体放在地点里的哪一格、哪一角。</p>`)}
        ${fieldRow("备注", `<textarea name="note"></textarea>`)}
        <div class="form-actions"><button class="primary" ${locations.length ? "" : "disabled"}>新增批次</button></div>
      </form>
    </section>
    <section class="band">
      <h2>使用</h2>
      <form id="consumeForm" class="grid-form">
        ${consumeLocationSelect(data.batches)}
        <input name="quantity" type="number" min="0.01" step="0.01" placeholder="使用数量" required />
        <input name="note" placeholder="备注" />
        <button class="primary" ${canConsume ? "" : "disabled"}>扣减库存</button>
      </form>
    </section>
    <section class="band"><h2>批次</h2><div class="list">${data.batches.map(batchCard).join("") || empty("暂无批次")}</div></section>
    <section class="band"><h2>记录</h2><div class="timeline">${data.movements.map(movementRow).join("") || empty("暂无记录")}</div></section>`;
  bindDetailForms(item.id);
}

function batchCard(batch: Batch) {
  const image = batch.photoPath || batch.locationPhoto;
  return `
    <article class="batch-card">
      <div class="batch-photo">
        ${image ? `<img src="${image}" alt="" />` : `<span>无照片</span>`}
        <form class="batchPhotoForm" data-batch="${batch.id}">
          <input id="batch-photo-${batch.id}" name="photo" class="photo-input" type="file" accept="image/*" capture="environment" />
          <label class="photo-float-button" for="batch-photo-${batch.id}">${batch.photoPath ? "换照片" : "上传照片"}</label>
        </form>
      </div>
      <div>
        <strong>${escapeHtml(batch.locationName)}</strong>
        <span>${fmt(batch.currentQuantity)} / ${fmt(batch.initialQuantity)}</span>
        ${batch.cost > 0 ? `<span>花费 ${fmtMoney(batch.cost)}</span>` : ""}
        <p>${escapeHtml(batch.note || "")}</p>
      </div>
      <div class="batch-actions">
        <select data-move="${batch.id}">${locations.map((l) => `<option value="${l.id}" ${l.id === batch.locationId ? "selected" : ""}>${escapeHtml(l.name)}</option>`).join("")}</select>
      </div>
    </article>`;
}

function movementRow(m: Movement) {
  const map: Record<string, string> = { in: "入库", consume: "使用", move: "移动" };
  return `<p><strong>${map[m.type] || m.type}</strong> ${fmt(m.quantity)} · ${escapeHtml(m.userName)} · ${new Date(m.createdAt).toLocaleString()}</p>`;
}

function renderSearchResults(raw: string) {
  const query = raw.trim();
  const result = document.querySelector("#searchResult");
  if (!result) return;
  if (!query) {
    result.innerHTML = `<p class="hint">请输入条码或物品关键词。</p>`;
    return;
  }
  const normalized = query.toLocaleLowerCase();
  const exactBarcode = items.filter((item) => item.barcode && item.barcode === query);
  const matches = exactBarcode.length
    ? exactBarcode
    : items.filter((item) =>
        [item.name, item.brand, item.spec, item.category, item.owner, item.barcode].some((value) => value.toLocaleLowerCase().includes(normalized)),
      );
  result.innerHTML = matches.length ? `<div class="list">${matches.map(itemCard).join("")}</div>` : `<p class="hint">没有匹配到，可以在“物品”里新增或补充条码。</p>`;
  result.querySelectorAll<HTMLElement>("[data-item]").forEach((el) => {
    el.onclick = async () => {
      await openItem(Number(el.dataset.item));
    };
  });
}

function bindGlobal() {
  document.querySelectorAll<HTMLButtonElement>("[data-view]").forEach((btn) => {
    btn.onclick = () => {
      navigateTo(btn.dataset.view as ViewName);
    };
  });
  document.querySelector("#refreshBtn")?.addEventListener("click", async () => {
    const btn = document.querySelector<HTMLButtonElement>("#refreshBtn");
    try {
      if (btn) {
        btn.disabled = true;
        btn.textContent = "…";
      }
      if (currentUser?.role === "admin") {
        await loadUsers();
      } else {
        await loadData();
      }
      if (activeView !== "items") selectedItem = null;
      render();
      if (activeView === "items" && selectedItem) {
        await renderItemDetail(selectedItem);
      }
    } catch (err) {
      showError(err instanceof Error ? err.message : "刷新失败");
      render();
    }
  });
  document.querySelector("[data-scroll-low-stock]")?.addEventListener("click", () => {
    document.querySelector("#lowStockBand")?.scrollIntoView({ behavior: "smooth", block: "start" });
  });
  document.querySelector("#logoutBtn")?.addEventListener("click", async () => {
    await api("/api/logout", { method: "POST" });
    renderLogin();
  });
  document.querySelectorAll<HTMLButtonElement>("[data-theme-choice]").forEach((btn) => {
    btn.onclick = () => {
      const theme = btn.dataset.themeChoice === "classic" ? "classic" : "warm";
      currentTheme = theme;
      localStorage.setItem("inventory_theme", theme);
      applyTheme(theme);
      render();
    };
  });
  document.querySelector("#scanBarcodeBtn")?.addEventListener("click", () => scanBarcodeIntoInput("#itemBarcode"));
  document.querySelector("#scanSearchBarcodeBtn")?.addEventListener("click", async () => {
    const scanned = await scanBarcodeIntoInput("#quickSearchInput");
    if (scanned) {
      renderSearchResults(scanned);
    }
  });
  document.querySelector("#toggleItemFormBtn")?.addEventListener("click", () => {
    showItemForm = !showItemForm;
    render();
  });
  document.querySelector("#toggleLocationFormBtn")?.addEventListener("click", () => {
    showLocationForm = !showLocationForm;
    editingLocationID = null;
    render();
  });
  document.querySelectorAll<HTMLButtonElement>("[data-edit-location]").forEach((btn) => {
    btn.onclick = () => {
      const id = Number(btn.dataset.editLocation);
      editingLocationID = editingLocationID === id ? null : id;
      showLocationForm = false;
      render();
    };
  });
  document.querySelectorAll<HTMLButtonElement>("[data-cancel-location-edit]").forEach((btn) => {
    btn.onclick = () => {
      editingLocationID = null;
      render();
    };
  });
  document.querySelectorAll<HTMLSelectElement>("[data-fill-input]").forEach((select) => {
    select.onchange = () => {
      const input =
        select.closest(".combo-input")?.querySelector<HTMLInputElement>(`input[name="${select.dataset.fillInput}"]`) ||
        document.querySelector<HTMLInputElement>(`input[name="${select.dataset.fillInput}"]`);
      if (input && select.value) input.value = select.value;
      select.value = "";
    };
  });
  document.querySelectorAll<HTMLElement>("[data-item]").forEach((el) => {
    el.onclick = async () => {
      await openItem(Number(el.dataset.item));
    };
  });
  document.querySelectorAll<HTMLButtonElement>("[data-edit-item]").forEach((btn) => {
    btn.onclick = async (event) => {
      event.stopPropagation();
      const itemId = Number(btn.dataset.editItem);
      showItemEditForm = true;
      await openItem(itemId);
    };
  });
  document.querySelectorAll<HTMLButtonElement>("[data-delete-item]").forEach((btn) => {
    btn.onclick = async (event) => {
      event.stopPropagation();
      const item = items.find((candidate) => candidate.id === Number(btn.dataset.deleteItem));
      if (!item || !confirm(`删除“${item.name}”？相关批次和使用记录也会删除。`)) return;
      await api(`/api/items/${item.id}`, { method: "DELETE" });
      await loadData();
      render();
    };
  });
}

function bindForms() {
  form("memberForm", async (fd) => {
    const body = toObject(fd);
    if (!isPhone(String(body.phone || ""))) throw new Error("手机号必须是 11 位数字");
    await api("/api/users", { method: "POST", body: JSON.stringify(body) });
    await loadUsers();
    render();
  });
  document.querySelectorAll<HTMLFormElement>(".userEditForm").forEach((el) => {
    el.onsubmit = async (event) => {
      event.preventDefault();
      const submit = el.querySelector<HTMLButtonElement>('button[type="submit"], button:not([type])');
      const originalText = submit?.textContent || "";
      try {
        if (submit) {
          submit.disabled = true;
          submit.textContent = "保存中...";
        }
        const body = toObject(new FormData(el));
        if (!isPhone(String(body.phone || ""))) throw new Error("手机号必须是 11 位数字");
        await api(`/api/users/${el.dataset.user}`, { method: "PATCH", body: JSON.stringify(body) });
        await loadUsers();
        render();
      } catch (err) {
        showError(err instanceof Error ? err.message : "保存失败");
      } finally {
        if (submit) {
          submit.disabled = false;
          submit.textContent = originalText;
        }
      }
    };
  });
  form("itemForm", async (fd) => {
    await api("/api/items", { method: "POST", body: JSON.stringify(toObject(fd)) });
    await loadData();
    showItemForm = false;
    render();
  });
  form("locationForm", async (fd) => {
    await api("/api/locations", { method: "POST", body: JSON.stringify(toObject(fd)) });
    await loadData();
    showLocationForm = false;
    render();
  });
  document.querySelectorAll<HTMLFormElement>(".locationEditForm").forEach((el) => {
    el.onsubmit = async (event) => {
      event.preventDefault();
      const submit = el.querySelector<HTMLButtonElement>('button[type="submit"], button:not([type])');
      const originalText = submit?.textContent || "";
      try {
        if (submit) {
          submit.disabled = true;
          submit.textContent = "保存中...";
        }
        await api(`/api/locations/${el.dataset.location}`, { method: "PATCH", body: JSON.stringify(toObject(new FormData(el))) });
        await loadData();
        editingLocationID = null;
        render();
      } catch (err) {
        showError(err instanceof Error ? err.message : "保存失败");
      } finally {
        if (submit) {
          submit.disabled = false;
          submit.textContent = originalText;
        }
      }
    };
  });
  document.querySelector<HTMLFormElement>("#searchForm")?.addEventListener("submit", (event) => {
    event.preventDefault();
    const query = new FormData(event.currentTarget as HTMLFormElement).get("query");
    renderSearchResults(String(query || ""));
  });
  document.querySelectorAll<HTMLFormElement>(".photoForm").forEach((el) => {
    const input = el.querySelector<HTMLInputElement>('input[type="file"]');
    const trigger = el.querySelector<HTMLElement>(".photo-float-button");
    input?.addEventListener("change", async () => {
      const file = input?.files?.[0];
      if (!file) return;
      try {
        if (trigger) {
          trigger.classList.add("is-busy");
          trigger.textContent = "压缩中";
        }
        const compressed = await compressImage(file);
        const fd = new FormData();
        fd.set("photo", compressed, "location.jpg");
        if (trigger) trigger.textContent = "上传中";
        await api(`/api/locations/${el.dataset.location}/photo`, { method: "POST", body: fd });
        await loadData();
        render();
      } catch (err) {
        showError(err instanceof Error ? err.message : "上传失败");
      } finally {
        if (trigger) {
          trigger.classList.remove("is-busy");
          trigger.textContent = "上传照片";
        }
      }
    });
  });
}

function bindDetailForms(itemId: number) {
  document.querySelector("#backBtn")?.addEventListener("click", () => {
    navigateTo("items");
  });
  document.querySelector("#toggleEditItemBtn")?.addEventListener("click", async () => {
    showItemEditForm = !showItemEditForm;
    await renderItemDetail(itemId);
  });
  document.querySelector("#editScanBarcodeBtn")?.addEventListener("click", () => scanBarcodeIntoInput("#editItemBarcode"));
  form("editItemForm", async (fd) => {
    await api(`/api/items/${itemId}`, { method: "PATCH", body: JSON.stringify(toObject(fd)) });
    await loadData();
    showItemEditForm = false;
    await renderItemDetail(itemId);
  });
  form("batchForm", async (fd) => {
    const photo = fd.get("photo");
    fd.delete("photo");
    const body = toObject(fd);
    const batch = await api<Batch>(`/api/items/${itemId}/batches`, { method: "POST", body: JSON.stringify(body) });
    if (photo instanceof File && photo.size > 0) {
      await uploadBatchPhoto(batch.id, photo);
    }
    await loadData();
    await renderItemDetail(itemId);
  });
  form("consumeForm", async (fd) => {
    await api(`/api/items/${itemId}/consume`, { method: "POST", body: JSON.stringify(toObject(fd)) });
    await loadData();
    await renderItemDetail(itemId);
  });
  document.querySelectorAll<HTMLSelectElement>("[data-move]").forEach((sel) => {
    sel.onchange = async () => {
      await api(`/api/batches/${sel.dataset.move}/move`, { method: "POST", body: JSON.stringify({ locationId: Number(sel.value), note: "移动存放地点" }) });
      await renderItemDetail(itemId);
    };
  });
  document.querySelectorAll<HTMLFormElement>(".batchPhotoForm").forEach((el) => {
    const input = el.querySelector<HTMLInputElement>('input[type="file"]');
    const trigger = el.querySelector<HTMLElement>(".photo-float-button");
    input?.addEventListener("change", async () => {
      const file = input.files?.[0];
      if (!file) return;
      try {
        if (trigger) {
          trigger.classList.add("is-busy");
          trigger.textContent = "上传中";
        }
        await uploadBatchPhoto(Number(el.dataset.batch), file);
        await renderItemDetail(itemId);
      } catch (err) {
        showError(err instanceof Error ? err.message : "上传失败");
      } finally {
        if (trigger) {
          trigger.classList.remove("is-busy");
          trigger.textContent = "上传照片";
        }
      }
    });
  });
}

function navigateTo(view: ViewName, options: { replace?: boolean } = {}) {
  activeView = currentUser?.role === "admin" ? "accounts" : view;
  selectedItem = null;
  showItemEditForm = false;
  if (activeView !== "items") showItemForm = false;
  if (activeView !== "locations") {
    showLocationForm = false;
    editingLocationID = null;
  }
  render();
  updateHistoryState(options.replace);
}

async function openItem(itemId: number, options: { replace?: boolean } = {}) {
  activeView = "items";
  selectedItem = itemId;
  showLocationForm = false;
  editingLocationID = null;
  render();
  updateHistoryState(options.replace);
  await renderItemDetail(itemId);
}

function currentHistoryState(): AppHistoryState {
  return { view: activeView, itemId: selectedItem };
}

function replaceHistoryState() {
  history.replaceState(currentHistoryState(), "", location.href);
}

function updateHistoryState(replace = false) {
  if (suppressHistoryUpdate) return;
  const state = currentHistoryState();
  if (replace) {
    history.replaceState(state, "", location.href);
  } else {
    history.pushState(state, "", location.href);
  }
}

window.addEventListener("popstate", async (event) => {
  const state = (event.state || { view: "home", itemId: null }) as AppHistoryState;
  suppressHistoryUpdate = true;
  try {
    activeView = currentUser?.role === "admin" ? "accounts" : state.view || "home";
    selectedItem = state.itemId || null;
    showItemForm = false;
    showItemEditForm = false;
    showLocationForm = false;
    editingLocationID = null;
    render();
    if (activeView === "items" && selectedItem) {
      await renderItemDetail(selectedItem);
    }
  } finally {
    suppressHistoryUpdate = false;
  }
});

function form(id: string, handler: (fd: FormData) => Promise<void>) {
  const el = document.querySelector<HTMLFormElement>(`#${id}`);
  if (!el) return;
  el.onsubmit = async (event) => {
    event.preventDefault();
    const submit = el.querySelector<HTMLButtonElement>('button[type="submit"], button:not([type])');
    const originalText = submit?.textContent || "";
    try {
      if (submit) {
        submit.disabled = true;
        submit.textContent = "处理中...";
      }
      await handler(new FormData(el));
      el.reset();
    } catch (err) {
      showError(err instanceof Error ? err.message : "操作失败");
    } finally {
      if (submit) {
        submit.disabled = false;
        submit.textContent = originalText;
      }
    }
  };
}

function toObject(fd: FormData): Record<string, string | number | boolean> {
  const numericFields = new Set(["cost", "minStock", "initialQuantity", "quantity", "locationId"]);
  const obj: Record<string, string | number | boolean> = {};
  fd.forEach((value, key) => {
    const text = String(value);
    obj[key] = numericFields.has(key) && /^-?\d+(\.\d+)?$/.test(text) ? Number(text) : text;
  });
  return obj;
}

function fieldRow(label: string, control: string) {
  return `<label class="field-row"><span>${escapeHtml(label)}</span><div>${control}</div></label>`;
}

function itemFormFields(item?: Item) {
  const barcodeInputID = item ? "editItemBarcode" : "itemBarcode";
  const scanButtonID = item ? "editScanBarcodeBtn" : "scanBarcodeBtn";
  return `
    ${fieldRow("物品名称", historyInput("name", "itemNameHistory", uniqueValues(items.map((candidate) => candidate.name)), true, item?.name))}
    ${fieldRow("使用方", ownerSelect(item?.owner))}
    ${fieldRow("分类", `${historyInput("category", "itemCategoryHistory", uniqueValues(items.map((candidate) => candidate.category)), false, item?.category)}<p id="categoryHint" class="field-hint">例如：清洁、厨房、纸品、洗护、药品。</p>`)}
    ${fieldRow("品牌", historyInput("brand", "itemBrandHistory", uniqueValues(items.map((candidate) => candidate.brand)), false, item?.brand))}
    ${fieldRow("规格", `<input name="spec" value="${escapeHtml(item?.spec || "")}" aria-describedby="specHint" /><p id="specHint" class="field-hint">例如：2kg/瓶、3层100抽/包、45cm x 50cm 100只/卷。</p>`)}
    ${fieldRow("单位", historyInput("unit", "itemUnitHistory", uniqueValues([...defaultUnits, ...items.map((candidate) => candidate.unit)]), true, item?.unit || "件"))}
    ${fieldRow("条码", `<div class="input-action"><input id="${barcodeInputID}" name="barcode" value="${escapeHtml(item?.barcode || "")}" inputmode="numeric" /><button type="button" id="${scanButtonID}">扫码</button></div>`)}
    ${fieldRow("最低库存", `<input name="minStock" type="number" step="0.01" min="0" value="${item?.minStock ?? 0}" aria-describedby="minStockHint" /><p id="minStockHint" class="field-hint">库存小于或等于这个数量时，会出现在首页“待补货”。</p>`)}
    ${fieldRow("备注", `<textarea name="note">${escapeHtml(item?.note || "")}</textarea>`)}`;
}

function historyInput(name: string, selectID: string, values: string[], required: boolean, value = "") {
  const hasHistory = values.length > 0;
  return `
    <div class="combo-input">
      <input name="${name}" value="${escapeHtml(value)}" ${required ? "required" : ""} />
      <div class="combo-history ${hasHistory ? "" : "disabled"}" aria-hidden="true">
        <span>常用</span>
        <span class="combo-arrow">⌄</span>
      </div>
      <select id="${selectID}" class="combo-select" data-fill-input="${name}" aria-label="选择常用输入" ${hasHistory ? "" : "disabled"}>
        <option value="">常用</option>
        ${values.map((value) => `<option value="${escapeHtml(value)}">${escapeHtml(value)}</option>`).join("")}
      </select>
    </div>`;
}

function ownerSelect(value = "") {
  return `<select name="owner"><option value="">选择使用方</option>${owners.map((owner) => `<option value="${escapeHtml(owner)}" ${owner === value ? "selected" : ""}>${escapeHtml(owner)}</option>`).join("")}</select>`;
}

function locationSelect() {
  if (!locations.length) {
    return `<div class="missing-field">还没有存放地点。请先到“地点”页添加，例如“厨房柜子第二层”。</div>`;
  }
  return `<select name="locationId" required><option value="">选择存放地点</option>${locations.map((l) => `<option value="${l.id}">${escapeHtml(l.name)}</option>`).join("")}</select>`;
}

function consumeLocationSelect(batches: Batch[]) {
  const stockByLocation = new Map<number, { name: string; quantity: number }>();
  for (const batch of batches) {
    if (batch.status === "done" || batch.currentQuantity <= 0) continue;
    const current = stockByLocation.get(batch.locationId) || { name: batch.locationName, quantity: 0 };
    current.quantity += batch.currentQuantity;
    stockByLocation.set(batch.locationId, current);
  }
  const options = Array.from(stockByLocation.entries()).map(([id, loc]) => `<option value="${id}">${escapeHtml(loc.name)} · 余 ${fmt(loc.quantity)}</option>`);
  if (!options.length) {
    return `<div class="missing-field">当前没有可扣减库存。请先入库。</div>`;
  }
  return `<select name="locationId" required><option value="">选择使用地点</option>${options.join("")}</select>`;
}

async function uploadBatchPhoto(batchID: number, file: File) {
  const compressed = await compressImage(file);
  const fd = new FormData();
  fd.set("photo", compressed, "batch-location.jpg");
  await api(`/api/batches/${batchID}/photo`, { method: "POST", body: fd });
}

async function compressImage(file: File) {
  if (!file.type.startsWith("image/")) {
    throw new Error("只能上传图片。");
  }
  const image = await loadImage(file);
  const maxSide = 1280;
  const scale = Math.min(1, maxSide / Math.max(image.naturalWidth, image.naturalHeight));
  const width = Math.max(1, Math.round(image.naturalWidth * scale));
  const height = Math.max(1, Math.round(image.naturalHeight * scale));
  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("当前浏览器无法压缩图片。");
  ctx.drawImage(image, 0, 0, width, height);
  const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/jpeg", 0.75));
  URL.revokeObjectURL(image.src);
  if (!blob) throw new Error("图片压缩失败。");
  return blob.size < file.size ? blob : file;
}

function loadImage(file: File) {
  return new Promise<HTMLImageElement>((resolve, reject) => {
    const image = new Image();
    image.onload = () => resolve(image);
    image.onerror = () => {
      URL.revokeObjectURL(image.src);
      reject(new Error("图片读取失败。"));
    };
    image.src = URL.createObjectURL(file);
  });
}

async function scanBarcodeIntoInput(selector: string) {
  const barcodeInput = document.querySelector<HTMLInputElement>(selector);
  if (!barcodeInput) return;
  const BarcodeDetectorClass = (window as Window & { BarcodeDetector?: BarcodeDetectorCtor }).BarcodeDetector;
  if (!BarcodeDetectorClass || !navigator.mediaDevices?.getUserMedia) {
    return scanBarcodeFromPhoto(barcodeInput);
  }

  let stream: MediaStream | null = null;
  let overlay: HTMLDivElement | null = null;
  try {
    stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: "environment" } });
    const preview = createScanPreview();
    overlay = preview.overlay;
    const video = preview.video;
    video.srcObject = stream;
    video.muted = true;
    video.playsInline = true;
    await video.play();
    const detector = new BarcodeDetectorClass({ formats: ["ean_13", "ean_8", "upc_a", "upc_e", "code_128"] });
    const startedAt = Date.now();
    while (Date.now() - startedAt < 10000) {
      if (preview.cancelled()) return "";
      const codes = await detector.detect(video);
      if (codes[0]?.rawValue) {
        barcodeInput.value = codes[0].rawValue;
        return codes[0].rawValue;
      }
      await wait(250);
    }
    showError("没有识别到条码，可以调整距离后再试，或手动输入。");
  } catch {
    return scanBarcodeFromPhoto(barcodeInput);
  } finally {
    stream?.getTracks().forEach((track) => track.stop());
    overlay?.remove();
  }
  return "";
}

function createScanPreview() {
  let isCancelled = false;
  const overlay = document.createElement("div");
  overlay.className = "scan-overlay";
  overlay.innerHTML = `
    <div class="scan-panel">
      <div class="scan-header">
        <div>
          <strong>对准条码</strong>
          <span>让条码尽量放在框内，保持清晰和稳定。</span>
        </div>
        <button type="button" class="small" data-scan-cancel>取消</button>
      </div>
      <div class="scan-video-wrap">
        <video class="scan-video" autoplay muted playsinline></video>
        <div class="scan-frame"></div>
      </div>
    </div>`;
  document.body.append(overlay);
  overlay.querySelector<HTMLButtonElement>("[data-scan-cancel]")?.addEventListener("click", () => {
    isCancelled = true;
  });
  const video = overlay.querySelector<HTMLVideoElement>("video")!;
  return { overlay, video, cancelled: () => isCancelled };
}

function scanBarcodeFromPhoto(barcodeInput: HTMLInputElement) {
  return new Promise<string>((resolve) => {
    const picker = document.createElement("input");
    picker.type = "file";
    picker.accept = "image/*";
    picker.setAttribute("capture", "environment");
    picker.className = "visually-hidden-file";
    document.body.append(picker);

    picker.onchange = async () => {
      const file = picker.files?.[0];
      if (!file) {
        picker.remove();
        resolve("");
        return;
      }
      if (!file.type.startsWith("image/")) {
        picker.remove();
        showError("请选择或拍摄图片。");
        resolve("");
        return;
      }
      const url = URL.createObjectURL(file);
      try {
        const reader = await loadImageBarcodeReader();
        const result = await reader.decodeFromImageUrl(url);
        const code = result.getText();
        barcodeInput.value = code;
        resolve(code);
      } catch {
        showError("没有从照片中识别到条码，请让条码占满画面并保持清晰。");
        resolve("");
      } finally {
        URL.revokeObjectURL(url);
        picker.remove();
      }
    };

    picker.click();
  });
}

async function loadImageBarcodeReader() {
  imageBarcodeReaderPromise ??= import("@zxing/browser").then(({ BrowserMultiFormatReader }) => new BrowserMultiFormatReader());
  return imageBarcodeReaderPromise;
}

function wait(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function beginNetwork() {
  pendingRequests += 1;
  renderNetworkStatus();
}

function endNetwork() {
  pendingRequests = Math.max(0, pendingRequests - 1);
  renderNetworkStatus();
}

function renderNetworkStatus() {
  let indicator = document.querySelector<HTMLDivElement>("#networkStatus");
  if (pendingRequests === 0) {
    indicator?.remove();
    return;
  }
  if (!indicator) {
    indicator = document.createElement("div");
    indicator.id = "networkStatus";
    indicator.className = "network-status";
    indicator.innerHTML = `<span class="spinner" aria-hidden="true"></span><span>正在请求...</span>`;
    document.body.append(indicator);
  }
}

function showError(message: string) {
  const old = document.querySelector("#errorToast");
  old?.remove();
  const toast = document.createElement("div");
  toast.id = "errorToast";
  toast.className = "error-toast";
  toast.textContent = message;
  document.body.append(toast);
  window.setTimeout(() => toast.remove(), 3600);
}

function uniqueValues(values: string[]) {
  return Array.from(new Set(values.map((value) => value.trim()).filter(Boolean))).sort((a, b) => a.localeCompare(b, "zh-CN"));
}

function isValidLogin(raw: string) {
  return raw === "admin" || isPhone(raw);
}

function isPhone(raw: string) {
  return /^\d{11}$/.test(raw);
}

function readTheme(): ThemeName {
  return localStorage.getItem("inventory_theme") === "classic" ? "classic" : "warm";
}

function applyTheme(theme: ThemeName) {
  document.body.dataset.theme = theme;
}

function empty(text: string) {
  return `<p class="empty">${text}</p>`;
}

function fmt(n: number) {
  return Number(n || 0).toLocaleString("zh-CN", { maximumFractionDigits: 2 });
}

function fmtMoney(n: number) {
  return `${Number(n || 0).toLocaleString("zh-CN", { minimumFractionDigits: 2, maximumFractionDigits: 2 })} 人民币`;
}

function escapeHtml(raw: string) {
  return raw.replace(/[&<>"']/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#039;" })[ch]!);
}

boot();
