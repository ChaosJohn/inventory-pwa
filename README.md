# 家用库存 PWA

一个面向家庭多人使用的日常用品进出库管理 app。后端使用 Go + SQLite，前端使用 Vite + TypeScript + 原生 DOM，可通过手机浏览器访问。

## 功能

- 物品管理：名称、分类、品牌、规格、单位、条码、最低库存。
- 存放地点：名称、描述、照片上传。
- 批次库存：每次购买形成一个批次，记录位置和数量。
- 使用记录：支持扣减数量、移动地点。
- 低库存提醒：根据总库存和最低库存实时计算。
- 登录：默认管理员账号 `admin`，面向家庭自用。

## 运行

安装依赖并构建前端：

```bash
npm install
npm run build
```

启动后端：

```bash
INVENTORY_ADMIN_PASSWORD=admin123 go run ./cmd/server -addr :8080
```

然后访问：

```text
http://localhost:8080
```

如果没有设置 `INVENTORY_ADMIN_PASSWORD`，首次启动会在终端打印一个随机 admin 密码。

## 数据

- SQLite 数据库默认在 `data/inventory.db`。
- 地点照片默认保存在 `data/uploads/locations/`。
- 备份时建议同时备份整个 `data/` 目录。

## 开发命令

```bash
go test ./...
npm run build
```

前端开发服务器：

```bash
npm run dev
```

后端默认服务 `web/dist` 中的生产前端构建产物。
