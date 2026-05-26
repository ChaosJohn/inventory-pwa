package store

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var ErrNotFound = errors.New("not found")
var ErrDuplicateItemName = errors.New("item name already exists")
var ErrDuplicateUser = errors.New("user already exists")

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

func Migrate(db *sql.DB) error {
	schema := []string{
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'member', created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id)`,
		`CREATE TABLE IF NOT EXISTS items (
			id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, category TEXT NOT NULL DEFAULT '', brand TEXT NOT NULL DEFAULT '',
			spec TEXT NOT NULL DEFAULT '', unit TEXT NOT NULL DEFAULT '件', barcode TEXT NOT NULL DEFAULT '', min_stock REAL NOT NULL DEFAULT 0,
			note TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_items_barcode ON items(barcode)`,
		`DROP INDEX IF EXISTS idx_items_name_unique`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_items_identity_unique ON items(name COLLATE NOCASE, brand COLLATE NOCASE, spec COLLATE NOCASE)`,
		`CREATE TABLE IF NOT EXISTS locations (
			id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', photo_path TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS location_photos (
			id INTEGER PRIMARY KEY AUTOINCREMENT, location_id INTEGER NOT NULL REFERENCES locations(id) ON DELETE CASCADE,
			path TEXT NOT NULL, mime_type TEXT NOT NULL, size INTEGER NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS stock_batches (
			id INTEGER PRIMARY KEY AUTOINCREMENT, item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
			location_id INTEGER NOT NULL REFERENCES locations(id), initial_quantity REAL NOT NULL, current_quantity REAL NOT NULL,
			opened INTEGER NOT NULL DEFAULT 0, purchased_at DATETIME NOT NULL, expires_at DATETIME NULL, status TEXT NOT NULL DEFAULT 'active',
			note TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS stock_movements (
			id INTEGER PRIMARY KEY AUTOINCREMENT, item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
			batch_id INTEGER REFERENCES stock_batches(id), user_id INTEGER NOT NULL REFERENCES users(id), type TEXT NOT NULL,
			quantity REAL NOT NULL DEFAULT 0, from_location_id INTEGER, to_location_id INTEGER, note TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	}
	for _, q := range schema {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	if err := ensureColumn(db, "items", "owner", `ALTER TABLE items ADD COLUMN owner TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "stock_batches", "cost", `ALTER TABLE stock_batches ADD COLUMN cost REAL NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := ensureColumn(db, "stock_batches", "photo_path", `ALTER TABLE stock_batches ADD COLUMN photo_path TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "users", "phone", `ALTER TABLE users ADD COLUMN phone TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "users", "status", `ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'active'`); err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE users SET phone='admin', status='active' WHERE username='admin' AND phone=''`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_phone_unique ON users(phone) WHERE phone!=''`); err != nil {
		return err
	}
	return nil
}

func (s *Store) CreateUser(name, username, phone, password, role, status string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if status == "" {
		status = "active"
	}
	_, err = s.db.Exec(`INSERT INTO users(name, username, phone, password_hash, role, status) VALUES(?,?,?,?,?,?)`, name, username, phone, string(hash), role, status)
	if isConstraintErr(err) {
		return ErrDuplicateUser
	}
	return err
}

func ensureColumn(db *sql.DB, table, column, alterSQL string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec(alterSQL)
	return err
}

func (s *Store) UserByUsername(username string) (User, error) {
	var u User
	err := s.db.QueryRow(`SELECT id,name,username,phone,password_hash,role,status,created_at FROM users WHERE username=?`, username).
		Scan(&u.ID, &u.Name, &u.Username, &u.Phone, &u.PasswordHash, &u.Role, &u.Status, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

func (s *Store) UserByLogin(login string) (User, error) {
	if login == "admin" {
		return s.UserByUsername("admin")
	}
	var u User
	err := s.db.QueryRow(`SELECT id,name,username,phone,password_hash,role,status,created_at FROM users WHERE phone=?`, login).
		Scan(&u.ID, &u.Name, &u.Username, &u.Phone, &u.PasswordHash, &u.Role, &u.Status, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

func (s *Store) UserByID(id int64) (User, error) {
	var u User
	err := s.db.QueryRow(`SELECT id,name,username,phone,password_hash,role,status,created_at FROM users WHERE id=?`, id).
		Scan(&u.ID, &u.Name, &u.Username, &u.Phone, &u.PasswordHash, &u.Role, &u.Status, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT id,name,username,phone,password_hash,role,status,created_at FROM users ORDER BY role, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Username, &u.Phone, &u.PasswordHash, &u.Role, &u.Status, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) SaveMember(u User, password string) (User, error) {
	u.Username = strings.TrimSpace(u.Username)
	u.Phone = strings.TrimSpace(u.Phone)
	u.Name = strings.TrimSpace(u.Name)
	if u.Name == "" {
		u.Name = u.Username
	}
	u.Role = "member"
	if u.Status == "" {
		u.Status = "active"
	}
	if u.ID == 0 {
		if password == "" {
			return User{}, fmt.Errorf("密码不能为空")
		}
		if err := s.CreateUser(u.Name, u.Username, u.Phone, password, u.Role, u.Status); err != nil {
			return User{}, err
		}
		return s.UserByUsername(u.Username)
	}
	existing, err := s.UserByID(u.ID)
	if err != nil {
		return User{}, err
	}
	if existing.Role == "admin" {
		return User{}, fmt.Errorf("不能在系统里修改 admin")
	}
	if password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return User{}, err
		}
		_, err = s.db.Exec(`UPDATE users SET name=?, username=?, phone=?, password_hash=?, status=? WHERE id=? AND role='member'`, u.Name, u.Username, u.Phone, string(hash), u.Status, u.ID)
		if isConstraintErr(err) {
			return User{}, ErrDuplicateUser
		}
		if err != nil {
			return User{}, err
		}
	} else {
		_, err = s.db.Exec(`UPDATE users SET name=?, username=?, phone=?, status=? WHERE id=? AND role='member'`, u.Name, u.Username, u.Phone, u.Status, u.ID)
		if isConstraintErr(err) {
			return User{}, ErrDuplicateUser
		}
		if err != nil {
			return User{}, err
		}
	}
	return s.UserByID(u.ID)
}

func (s *Store) UpdateUserPassword(username, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE users SET password_hash=? WHERE username=?`, string(hash), username)
	return err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (s *Store) CreateSession(token string, userID int64, expiresAt time.Time) error {
	_, err := s.db.Exec(`INSERT INTO sessions(token,user_id,expires_at) VALUES(?,?,?)`, token, userID, expiresAt)
	return err
}

func (s *Store) SessionUser(token string) (User, error) {
	var userID int64
	var expiresAt time.Time
	err := s.db.QueryRow(`SELECT user_id,expires_at FROM sessions WHERE token=?`, token).Scan(&userID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	if time.Now().After(expiresAt) {
		_ = s.DeleteSession(token)
		return User{}, ErrNotFound
	}
	return s.UserByID(userID)
}

func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token=?`, token)
	return err
}

func (s *Store) DeleteExpiredSessions(now time.Time) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at<=?`, now)
	return err
}

func (s *Store) Dashboard() (Dashboard, error) {
	items, err := s.ListItems("", "", false)
	if err != nil {
		return Dashboard{}, err
	}
	low, err := s.ListItems("", "", true)
	if err != nil {
		return Dashboard{}, err
	}
	recent, err := s.RecentMovements(8)
	return Dashboard{LowStock: low, Recent: recent, Items: items}, err
}

func (s *Store) ListItems(search, category string, lowOnly bool) ([]Item, error) {
	args := []any{}
	where := []string{"1=1"}
	out := []Item{}
	if search != "" {
		where = append(where, "(i.name LIKE ? OR i.barcode LIKE ? OR i.brand LIKE ?)")
		term := "%" + search + "%"
		args = append(args, term, term, term)
	}
	if category != "" {
		where = append(where, "i.category=?")
		args = append(args, category)
	}
	q := `SELECT i.id,i.name,i.category,i.owner,i.brand,i.spec,i.unit,i.barcode,i.min_stock,i.note,i.created_at,i.updated_at,
		COALESCE(SUM(CASE WHEN b.status!='done' THEN b.current_quantity ELSE 0 END),0) total,
		COALESCE(SUM(b.cost),0) total_cost
		FROM items i LEFT JOIN stock_batches b ON b.item_id=i.id WHERE ` + strings.Join(where, " AND ") + ` GROUP BY i.id ORDER BY i.updated_at DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Name, &it.Category, &it.Owner, &it.Brand, &it.Spec, &it.Unit, &it.Barcode, &it.MinStock, &it.Note, &it.CreatedAt, &it.UpdatedAt, &it.TotalStock, &it.TotalCost); err != nil {
			return nil, err
		}
		it.IsLowStock = it.TotalStock <= it.MinStock
		if !lowOnly || it.IsLowStock {
			out = append(out, it)
		}
	}
	return out, rows.Err()
}

func (s *Store) GetItem(id int64) (Item, []Batch, []Movement, error) {
	items, err := s.ListItems("", "", false)
	if err != nil {
		return Item{}, nil, nil, err
	}
	var found Item
	for _, it := range items {
		if it.ID == id {
			found = it
			break
		}
	}
	if found.ID == 0 {
		return Item{}, nil, nil, ErrNotFound
	}
	batches, err := s.BatchesForItem(id)
	if err != nil {
		return Item{}, nil, nil, err
	}
	movements, err := s.MovementsForItem(id)
	return found, batches, movements, err
}

func (s *Store) SaveItem(it Item) (Item, error) {
	it.Name = strings.TrimSpace(it.Name)
	it.Category = strings.TrimSpace(it.Category)
	it.Owner = strings.TrimSpace(it.Owner)
	it.Brand = strings.TrimSpace(it.Brand)
	it.Spec = strings.TrimSpace(it.Spec)
	it.Unit = strings.TrimSpace(it.Unit)
	it.Barcode = strings.TrimSpace(it.Barcode)
	it.Note = strings.TrimSpace(it.Note)
	if strings.TrimSpace(it.Unit) == "" {
		it.Unit = "件"
	}
	if err := s.ensureUniqueItemIdentity(it.ID, it.Name, it.Brand, it.Spec); err != nil {
		return it, err
	}
	if it.ID == 0 {
		res, err := s.db.Exec(`INSERT INTO items(name,category,owner,brand,spec,unit,barcode,min_stock,note) VALUES(?,?,?,?,?,?,?,?,?)`,
			it.Name, it.Category, it.Owner, it.Brand, it.Spec, it.Unit, it.Barcode, it.MinStock, it.Note)
		if err != nil {
			return it, err
		}
		it.ID, _ = res.LastInsertId()
	} else {
		_, err := s.db.Exec(`UPDATE items SET name=?,category=?,owner=?,brand=?,spec=?,unit=?,barcode=?,min_stock=?,note=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`,
			it.Name, it.Category, it.Owner, it.Brand, it.Spec, it.Unit, it.Barcode, it.MinStock, it.Note, it.ID)
		if err != nil {
			return it, err
		}
	}
	item, _, _, err := s.GetItem(it.ID)
	return item, err
}

func (s *Store) DeleteItem(id int64) error {
	res, err := s.db.Exec(`DELETE FROM items WHERE id=?`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ensureUniqueItemIdentity(id int64, name, brand, spec string) error {
	var existing int64
	err := s.db.QueryRow(`SELECT id FROM items WHERE name=? COLLATE NOCASE AND brand=? COLLATE NOCASE AND spec=? COLLATE NOCASE AND id!=? LIMIT 1`, name, brand, spec, id).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return ErrDuplicateItemName
}

func isConstraintErr(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "constraint")
}

func (s *Store) Locations() ([]Location, error) {
	rows, err := s.db.Query(`SELECT id,name,description,photo_path,created_at,updated_at FROM locations ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Location{}
	for rows.Next() {
		var l Location
		if err := rows.Scan(&l.ID, &l.Name, &l.Description, &l.PhotoPath, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) SaveLocation(l Location) (Location, error) {
	if l.ID == 0 {
		res, err := s.db.Exec(`INSERT INTO locations(name,description) VALUES(?,?)`, l.Name, l.Description)
		if err != nil {
			return l, err
		}
		l.ID, _ = res.LastInsertId()
	} else {
		_, err := s.db.Exec(`UPDATE locations SET name=?,description=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, l.Name, l.Description, l.ID)
		if err != nil {
			return l, err
		}
	}
	return s.Location(l.ID)
}

func (s *Store) Location(id int64) (Location, error) {
	var l Location
	err := s.db.QueryRow(`SELECT id,name,description,photo_path,created_at,updated_at FROM locations WHERE id=?`, id).
		Scan(&l.ID, &l.Name, &l.Description, &l.PhotoPath, &l.CreatedAt, &l.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return l, ErrNotFound
	}
	return l, err
}

func (s *Store) SetLocationPhoto(id int64, path, mime string, size int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO location_photos(location_id,path,mime_type,size) VALUES(?,?,?,?)`, id, path, mime, size); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE locations SET photo_path=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, path, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ClearLocationPhoto(id int64) error {
	_, err := s.db.Exec(`UPDATE locations SET photo_path='',updated_at=CURRENT_TIMESTAMP WHERE id=?`, id)
	return err
}

func (s *Store) AddBatch(userID int64, b Batch) (Batch, error) {
	if b.PurchasedAt.IsZero() {
		b.PurchasedAt = time.Now()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return b, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO stock_batches(item_id,location_id,initial_quantity,current_quantity,cost,opened,purchased_at,expires_at,status,note)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, b.ItemID, b.LocationID, b.InitialQuantity, b.InitialQuantity, b.Cost, 0, b.PurchasedAt, b.ExpiresAt, statusForQty(b.InitialQuantity), b.Note)
	if err != nil {
		return b, err
	}
	b.ID, _ = res.LastInsertId()
	if _, err := tx.Exec(`INSERT INTO stock_movements(item_id,batch_id,user_id,type,quantity,to_location_id,note) VALUES(?,?,?,?,?,?,?)`,
		b.ItemID, b.ID, userID, "in", b.InitialQuantity, b.LocationID, "入库"); err != nil {
		return b, err
	}
	if err := touchItemTx(tx, b.ItemID); err != nil {
		return b, err
	}
	if err := tx.Commit(); err != nil {
		return b, err
	}
	return s.Batch(b.ID)
}

func (s *Store) Batch(id int64) (Batch, error) {
	var b Batch
	var expires sql.NullTime
	err := s.db.QueryRow(`SELECT b.id,b.item_id,i.name,b.location_id,l.name,l.photo_path,b.photo_path,b.initial_quantity,b.current_quantity,b.cost,b.purchased_at,b.expires_at,b.status,b.note,b.created_at,b.updated_at
		FROM stock_batches b JOIN items i ON i.id=b.item_id JOIN locations l ON l.id=b.location_id WHERE b.id=?`, id).
		Scan(&b.ID, &b.ItemID, &b.ItemName, &b.LocationID, &b.LocationName, &b.LocationPhoto, &b.PhotoPath, &b.InitialQuantity, &b.CurrentQuantity, &b.Cost, &b.PurchasedAt, &expires, &b.Status, &b.Note, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	if expires.Valid {
		b.ExpiresAt = &expires.Time
	}
	return b, err
}

func (s *Store) BatchesForItem(itemID int64) ([]Batch, error) {
	rows, err := s.db.Query(`SELECT b.id,b.item_id,i.name,b.location_id,l.name,l.photo_path,b.photo_path,b.initial_quantity,b.current_quantity,b.cost,b.purchased_at,b.expires_at,b.status,b.note,b.created_at,b.updated_at
		FROM stock_batches b JOIN items i ON i.id=b.item_id JOIN locations l ON l.id=b.location_id WHERE b.item_id=? ORDER BY b.status,b.purchased_at,b.id`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Batch{}
	for rows.Next() {
		var b Batch
		var expires sql.NullTime
		if err := rows.Scan(&b.ID, &b.ItemID, &b.ItemName, &b.LocationID, &b.LocationName, &b.LocationPhoto, &b.PhotoPath, &b.InitialQuantity, &b.CurrentQuantity, &b.Cost, &b.PurchasedAt, &expires, &b.Status, &b.Note, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		if expires.Valid {
			b.ExpiresAt = &expires.Time
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) SetBatchPhoto(id int64, path string) error {
	res, err := s.db.Exec(`UPDATE stock_batches SET photo_path=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, path, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) MoveBatch(userID, batchID, locationID int64, note string) error {
	b, err := s.Batch(batchID)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE stock_batches SET location_id=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, locationID, batchID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO stock_movements(item_id,batch_id,user_id,type,quantity,from_location_id,to_location_id,note) VALUES(?,?,?,?,?,?,?,?)`,
		b.ItemID, batchID, userID, "move", 0, b.LocationID, locationID, note); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Consume(userID, itemID, locationID int64, qty float64, note string) error {
	if qty <= 0 || math.IsNaN(qty) {
		return fmt.Errorf("quantity must be positive")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	args := []any{itemID}
	locationFilter := ""
	if locationID > 0 {
		locationFilter = " AND location_id=?"
		args = append(args, locationID)
	}
	rows, err := tx.Query(`SELECT id,current_quantity,location_id FROM stock_batches WHERE item_id=? AND status!='done' AND current_quantity>0`+locationFilter+` ORDER BY purchased_at,id`, args...)
	if err != nil {
		return err
	}
	type candidate struct {
		id         int64
		qty        float64
		locationID int64
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.qty, &c.locationID); err != nil {
			rows.Close()
			return err
		}
		cands = append(cands, c)
	}
	rows.Close()
	remaining := qty
	for _, c := range cands {
		if remaining <= 0 {
			break
		}
		take := math.Min(c.qty, remaining)
		nextQty := c.qty - take
		if _, err := tx.Exec(`UPDATE stock_batches SET current_quantity=?,status=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, nextQty, statusForQty(nextQty), c.id); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO stock_movements(item_id,batch_id,user_id,type,quantity,from_location_id,note) VALUES(?,?,?,?,?,?,?)`, itemID, c.id, userID, "consume", take, c.locationID, note); err != nil {
			return err
		}
		remaining -= take
	}
	if remaining > 0.000001 {
		return fmt.Errorf("not enough stock")
	}
	if err := touchItemTx(tx, itemID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RecentMovements(limit int) ([]Movement, error) {
	rows, err := s.db.Query(`SELECT m.id,m.item_id,m.batch_id,m.user_id,u.name,m.type,m.quantity,m.from_location_id,m.to_location_id,m.note,m.created_at
		FROM stock_movements m JOIN users u ON u.id=m.user_id ORDER BY m.created_at DESC,m.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMovements(rows)
}

func (s *Store) MovementsForItem(itemID int64) ([]Movement, error) {
	rows, err := s.db.Query(`SELECT m.id,m.item_id,m.batch_id,m.user_id,u.name,m.type,m.quantity,m.from_location_id,m.to_location_id,m.note,m.created_at
		FROM stock_movements m JOIN users u ON u.id=m.user_id WHERE m.item_id=? ORDER BY m.created_at DESC,m.id DESC`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMovements(rows)
}

func scanMovements(rows *sql.Rows) ([]Movement, error) {
	out := []Movement{}
	for rows.Next() {
		var m Movement
		var batch sql.NullInt64
		var from sql.NullInt64
		var to sql.NullInt64
		if err := rows.Scan(&m.ID, &m.ItemID, &batch, &m.UserID, &m.UserName, &m.Type, &m.Quantity, &from, &to, &m.Note, &m.CreatedAt); err != nil {
			return nil, err
		}
		if batch.Valid {
			m.BatchID = batch.Int64
		}
		if from.Valid {
			m.FromLocationID = &from.Int64
		}
		if to.Valid {
			m.ToLocationID = &to.Int64
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) ItemByBarcode(code string) (Item, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM items WHERE barcode=? AND barcode!='' LIMIT 1`, code).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, err
	}
	item, _, _, err := s.GetItem(id)
	return item, err
}

func touchItemTx(tx *sql.Tx, itemID int64) error {
	_, err := tx.Exec(`UPDATE items SET updated_at=CURRENT_TIMESTAMP WHERE id=?`, itemID)
	return err
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func statusForQty(q float64) string {
	if q <= 0 {
		return "done"
	}
	return "active"
}
