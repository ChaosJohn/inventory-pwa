package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"household-inventory/internal/store"
)

type Config struct {
	StaticDir  string
	UploadDir  string
	TLSEnabled bool
}

type Server struct {
	store *store.Store
	cfg   Config
}

func NewServer(st *store.Store, cfg Config) *Server {
	return &Server{store: st, cfg: cfg}
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(s.securityHeaders)

	r.Post("/api/login", s.login)
	r.Post("/api/logout", s.logout)
	r.Group(func(api chi.Router) {
		api.Use(s.requireUser)
		api.Get("/api/me", s.me)
		api.Group(func(admin chi.Router) {
			admin.Use(s.requireRole("admin"))
			admin.Get("/api/users", s.users)
			admin.Post("/api/users", s.saveUser)
			admin.Patch("/api/users/{id}", s.saveUser)
		})
		api.Group(func(member chi.Router) {
			member.Use(s.requireRole("member"))
			member.Get("/api/dashboard", s.dashboard)
			member.Get("/api/items", s.items)
			member.Post("/api/items", s.saveItem)
			member.Get("/api/items/{id}", s.itemDetail)
			member.Patch("/api/items/{id}", s.saveItem)
			member.Delete("/api/items/{id}", s.deleteItem)
			member.Post("/api/items/{id}/batches", s.addBatch)
			member.Post("/api/items/{id}/consume", s.consume)
			member.Get("/api/locations", s.locations)
			member.Post("/api/locations", s.saveLocation)
			member.Patch("/api/locations/{id}", s.saveLocation)
			member.Post("/api/locations/{id}/photo", s.uploadLocationPhoto)
			member.Delete("/api/locations/{id}/photo", s.deleteLocationPhoto)
			member.Post("/api/batches/{id}/move", s.moveBatch)
			member.Post("/api/batches/{id}/photo", s.uploadBatchPhoto)
			member.Get("/api/alerts/low-stock", s.lowStock)
			member.Get("/api/barcodes/{code}", s.barcode)
		})
	})
	r.Handle("/uploads/*", http.StripPrefix("/uploads/", http.FileServer(http.Dir(s.cfg.UploadDir))))
	r.Handle("/*", spaFileServer(s.cfg.StaticDir))
	return r
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if s.cfg.TLSEnabled {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Login    string `json:"login"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	login := strings.TrimSpace(req.Login)
	if login == "" {
		login = strings.TrimSpace(req.Username)
	}
	if !validLogin(login) {
		writeError(w, http.StatusBadRequest, "请输入 admin 或 11 位手机号")
		return
	}
	u, err := s.store.UserByLogin(login)
	if err != nil || !store.CheckPassword(u.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "账号或密码错误")
		return
	}
	if u.Status != "active" {
		writeError(w, http.StatusForbidden, "账号已停用")
		return
	}
	token := randomToken()
	maxAge := 86400 * 30
	if err := s.store.CreateSession(token, u.ID, time.Now().Add(time.Duration(maxAge)*time.Second)); err != nil {
		writeError(w, http.StatusInternalServerError, "创建登录状态失败")
		return
	}
	_ = s.store.DeleteExpiredSessions(time.Now())
	http.SetCookie(w, &http.Cookie{Name: "inventory_session", Value: token, Path: "/", HttpOnly: true, Secure: s.cfg.TLSEnabled, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
	writeJSON(w, u)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("inventory_session"); err == nil {
		_ = s.store.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "inventory_session", Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("inventory_session")
		if err != nil {
			writeError(w, http.StatusUnauthorized, "请先登录")
			return
		}
		u, err := s.store.SessionUser(c.Value)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "登录已过期")
			return
		}
		if u.Status != "active" {
			writeError(w, http.StatusForbidden, "账号已停用")
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u.ID, u.Role)))
	})
}

func (s *Server) requireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if userRole(r) != role {
				writeError(w, http.StatusForbidden, "没有权限")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u, err := s.store.UserByID(userID(r))
	respond(w, u, err)
}

func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers()
	respond(w, users, err)
}

func (s *Server) saveUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone    string `json:"phone"`
		Username string `json:"username"`
		Password string `json:"password"`
		Status   string `json:"status"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Phone = strings.TrimSpace(req.Phone)
	req.Username = strings.TrimSpace(req.Username)
	req.Status = strings.TrimSpace(req.Status)
	if !validPhone(req.Phone) {
		writeError(w, http.StatusBadRequest, "手机号必须是 11 位数字")
		return
	}
	if req.Username == "" {
		writeError(w, http.StatusBadRequest, "用户名不能为空")
		return
	}
	if req.Status == "" {
		req.Status = "active"
	}
	if req.Status != "active" && req.Status != "disabled" {
		writeError(w, http.StatusBadRequest, "账号状态不正确")
		return
	}
	u := store.User{
		ID:       parseID(chi.URLParam(r, "id")),
		Name:     req.Username,
		Username: req.Username,
		Phone:    req.Phone,
		Status:   req.Status,
	}
	saved, err := s.store.SaveMember(u, req.Password)
	respond(w, saved, err)
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	d, err := s.store.Dashboard()
	respond(w, d, err)
}

func (s *Server) items(w http.ResponseWriter, r *http.Request) {
	low := r.URL.Query().Get("low") == "1"
	items, err := s.store.ListItems(r.URL.Query().Get("q"), r.URL.Query().Get("category"), low)
	respond(w, items, err)
}

func (s *Server) saveItem(w http.ResponseWriter, r *http.Request) {
	var it store.Item
	if !decode(w, r, &it) {
		return
	}
	if id := chi.URLParam(r, "id"); id != "" {
		it.ID = parseID(id)
	}
	if strings.TrimSpace(it.Name) == "" {
		writeError(w, http.StatusBadRequest, "物品名称不能为空")
		return
	}
	saved, err := s.store.SaveItem(it)
	respond(w, saved, err)
}

func (s *Server) itemDetail(w http.ResponseWriter, r *http.Request) {
	item, batches, movements, err := s.store.GetItem(parseID(chi.URLParam(r, "id")))
	respond(w, map[string]any{"item": item, "batches": batches, "movements": movements}, err)
}

func (s *Server) deleteItem(w http.ResponseWriter, r *http.Request) {
	err := s.store.DeleteItem(parseID(chi.URLParam(r, "id")))
	respond(w, map[string]bool{"ok": true}, err)
}

func (s *Server) addBatch(w http.ResponseWriter, r *http.Request) {
	var req store.Batch
	if !decode(w, r, &req) {
		return
	}
	req.ItemID = parseID(chi.URLParam(r, "id"))
	if req.InitialQuantity <= 0 || req.LocationID == 0 {
		writeError(w, http.StatusBadRequest, "数量和存放地点必填")
		return
	}
	batch, err := s.store.AddBatch(userID(r), req)
	respond(w, batch, err)
}

func (s *Server) consume(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LocationID int64   `json:"locationId"`
		Quantity   float64 `json:"quantity"`
		Note       string  `json:"note"`
	}
	if !decode(w, r, &req) {
		return
	}
	err := s.store.Consume(userID(r), parseID(chi.URLParam(r, "id")), req.LocationID, req.Quantity, req.Note)
	respond(w, map[string]bool{"ok": true}, err)
}

func (s *Server) locations(w http.ResponseWriter, r *http.Request) {
	locations, err := s.store.Locations()
	respond(w, locations, err)
}

func (s *Server) saveLocation(w http.ResponseWriter, r *http.Request) {
	var loc store.Location
	if !decode(w, r, &loc) {
		return
	}
	if id := chi.URLParam(r, "id"); id != "" {
		loc.ID = parseID(id)
	}
	if strings.TrimSpace(loc.Name) == "" {
		writeError(w, http.StatusBadRequest, "地点名称不能为空")
		return
	}
	saved, err := s.store.SaveLocation(loc)
	respond(w, saved, err)
}

func (s *Server) uploadLocationPhoto(w http.ResponseWriter, r *http.Request) {
	id := parseID(chi.URLParam(r, "id"))
	publicPath, mime, size, err := s.saveUploadedImage(w, r, "locations", strconv.FormatInt(id, 10))
	if err != nil {
		return
	}
	err = s.store.SetLocationPhoto(id, publicPath, mime, size)
	respond(w, map[string]string{"photoPath": publicPath}, err)
}

func (s *Server) uploadBatchPhoto(w http.ResponseWriter, r *http.Request) {
	id := parseID(chi.URLParam(r, "id"))
	publicPath, _, _, err := s.saveUploadedImage(w, r, "batches", strconv.FormatInt(id, 10))
	if err != nil {
		return
	}
	err = s.store.SetBatchPhoto(id, publicPath)
	respond(w, map[string]string{"photoPath": publicPath}, err)
}

var allowedImageExt = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
}

func (s *Server) saveUploadedImage(w http.ResponseWriter, r *http.Request, kind string, prefix string) (string, string, int64, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "图片太大或格式错误")
		return "", "", 0, err
	}
	file, header, err := r.FormFile("photo")
	if err != nil {
		writeError(w, http.StatusBadRequest, "缺少图片")
		return "", "", 0, err
	}
	defer file.Close()
	if header.Size > 3<<20 {
		writeError(w, http.StatusBadRequest, "图片不能超过 3MB")
		return "", "", 0, errors.New("image too large")
	}
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	mime := http.DetectContentType(buf[:n])
	if !strings.HasPrefix(mime, "image/") {
		writeError(w, http.StatusBadRequest, "只能上传图片")
		return "", "", 0, errors.New("not image")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusBadRequest, "读取图片失败")
		return "", "", 0, err
	}
	dir := filepath.Join(s.cfg.UploadDir, kind)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return "", "", 0, err
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedImageExt[ext] {
		ext = ".jpg"
	}
	name := prefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 10) + ext
	dstPath := filepath.Join(dir, name)
	absDst, err := filepath.Abs(dstPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的文件路径")
		return "", "", 0, err
	}
	absUpload, err := filepath.Abs(s.cfg.UploadDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return "", "", 0, err
	}
	if !strings.HasPrefix(absDst, absUpload+string(filepath.Separator)) {
		writeError(w, http.StatusBadRequest, "无效的文件路径")
		return "", "", 0, errors.New("path traversal detected")
	}
	dst, err := os.Create(absDst)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return "", "", 0, err
	}
	size, copyErr := io.Copy(dst, file)
	closeErr := dst.Close()
	if copyErr != nil || closeErr != nil {
		writeError(w, http.StatusInternalServerError, "保存图片失败")
		return "", "", 0, errors.New("save image failed")
	}
	return "/uploads/" + kind + "/" + name, mime, size, nil
}

func (s *Server) deleteLocationPhoto(w http.ResponseWriter, r *http.Request) {
	err := s.store.ClearLocationPhoto(parseID(chi.URLParam(r, "id")))
	respond(w, map[string]bool{"ok": true}, err)
}

func (s *Server) moveBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LocationID int64  `json:"locationId"`
		Note       string `json:"note"`
	}
	if !decode(w, r, &req) {
		return
	}
	err := s.store.MoveBatch(userID(r), parseID(chi.URLParam(r, "id")), req.LocationID, req.Note)
	respond(w, map[string]bool{"ok": true}, err)
}

func (s *Server) lowStock(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListItems("", "", true)
	respond(w, items, err)
}

func (s *Server) barcode(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.ItemByBarcode(chi.URLParam(r, "code"))
	respond(w, item, err)
}

func respond(w http.ResponseWriter, data any, err error) {
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "没有找到记录")
			return
		}
		if errors.Is(err, store.ErrDuplicateItemName) {
			writeError(w, http.StatusConflict, "已存在相同名称、品牌和规格的物品，请直接进入已有物品入库")
			return
		}
		if errors.Is(err, store.ErrDuplicateUser) {
			writeError(w, http.StatusConflict, "手机号或用户名已存在")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, data)
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func parseID(raw string) int64 {
	id, _ := strconv.ParseInt(raw, 10, 64)
	return id
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func validLogin(raw string) bool {
	return raw == "admin" || validPhone(raw)
}

func validPhone(raw string) bool {
	if len(raw) != 11 {
		return false
	}
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func spaFileServer(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	absDir, _ := filepath.Abs(dir)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleaned := filepath.Clean(r.URL.Path)
		path := filepath.Join(absDir, cleaned)
		if !strings.HasPrefix(path, absDir+string(filepath.Separator)) && path != absDir {
			http.NotFound(w, r)
			return
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(absDir, "index.html"))
	})
}
