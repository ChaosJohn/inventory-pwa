package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"household-inventory/internal/app"
	"household-inventory/internal/store"

	_ "modernc.org/sqlite"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dbPath := flag.String("db", "data/inventory.db", "SQLite database path")
	staticDir := flag.String("static", "web/dist", "static frontend directory")
	uploadDir := flag.String("uploads", "data/uploads", "upload directory")
	tlsEnabled := flag.Bool("tls", false, "enable HTTPS using the default certificate files in ssl/")
	certFile := flag.String("cert", "", "TLS certificate file")
	keyFile := flag.String("key", "", "TLS private key file")
	flag.Parse()

	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(*uploadDir, 0o755); err != nil {
		log.Fatal(err)
	}

	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)
	if err := store.Migrate(db); err != nil {
		log.Fatal(err)
	}
	st := store.New(db)
	password, err := ensureAdmin(st)
	if err != nil {
		log.Fatal(err)
	}
	if password != "" {
		log.Printf("created default admin: username=admin password=%s", password)
	}

	server := app.NewServer(st, app.Config{
		StaticDir:  *staticDir,
		UploadDir:  *uploadDir,
		TLSEnabled: *tlsEnabled || *certFile != "" || *keyFile != "",
	})

	if *tlsEnabled && (*certFile == "" || *keyFile == "") {
		*certFile = firstNonEmpty(*certFile, "ssl/x.chaosjohn.com_ecc/fullchain.cer")
		*keyFile = firstNonEmpty(*keyFile, "ssl/x.chaosjohn.com_ecc/x.chaosjohn.com.key")
	}
	if *certFile != "" || *keyFile != "" {
		if *certFile == "" || *keyFile == "" {
			log.Fatal("both -cert and -key are required for HTTPS")
		}
		log.Printf("listening on https://localhost%s", normalizeAddr(*addr))
		log.Fatal(http.ListenAndServeTLS(*addr, *certFile, *keyFile, server.Routes()))
	}

	log.Printf("listening on http://localhost%s", normalizeAddr(*addr))
	log.Fatal(http.ListenAndServe(*addr, server.Routes()))
}

func ensureAdmin(st *store.Store) (string, error) {
	_, err := st.UserByUsername("admin")
	if err == nil {
		password := strings.TrimSpace(os.Getenv("INVENTORY_ADMIN_PASSWORD"))
		if password != "" {
			return "", st.UpdateUserPassword("admin", password)
		}
		return "", nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	password := strings.TrimSpace(os.Getenv("INVENTORY_ADMIN_PASSWORD"))
	if password == "" {
		password = fmt.Sprintf("admin-%d", time.Now().Unix()%1000000)
	}
	return password, st.CreateUser("Admin", "admin", "admin", password, "admin", "active")
}

func normalizeAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return addr
	}
	return "://" + addr
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
