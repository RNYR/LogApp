package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type Item struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	SN         string `json:"sn"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	SentDate   string `json:"sentDate"`
	ReturnDate string `json:"returnDate"`
	Note       string `json:"note"`
}

type EmailConfig struct {
	SMTPHost string `json:"smtp_host"`
	SMTPPort string `json:"smtp_port"`
	SMTPUser string `json:"smtp_user"`
	SMTPPass string `json:"smtp_pass"`
	SMTPFrom string `json:"smtp_from"`
	SMTPTo   string `json:"smtp_to"`
	SMTPCC   string `json:"smtp_cc"` // comma-separated
}

type StatusOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Cls   string `json:"cls"`
}

type Options struct {
	Types    []string       `json:"types"`
	Statuses []StatusOption `json:"statuses"`
}

type editorLockState struct {
	Holder    string
	Token     string
	ExpiresAt int64
	Mode      string // "admin" | "normal"
}

// ---------------------------------------------------------------------------
// Config (read from env at startup)
// ---------------------------------------------------------------------------

var (
	adminUsername string
	adminPassword string
	checkHour     int
)

// ---------------------------------------------------------------------------
// Global state
// ---------------------------------------------------------------------------

var (
	mu               sync.Mutex
	editorLock       editorLockState
	appBaseDir           string
	inventoryPath        string
	optionsPath          string
	emailConfigPath      string
	backupsDir           string
	hourlyBackupsDir     string
)

var defaultOptions = Options{
	Types: []string{
		"Camera", "Lens", "Adapter", "Light", "PC", "Router",
		"Power Supply", "Monitor", "Keyboard", "Mice", "Cable", "Module/Block",
	},
	Statuses: []StatusOption{
		{Value: "Feasibility", Label: "Feasibility", Cls: "status-feas"},
		{Value: "Devel", Label: "Devel", Cls: "status-devel"},
		{Value: "Sales", Label: "Sales", Cls: "status-sales"},
		{Value: "Customer", Label: "Customer On-Site", Cls: "status-customer"},
		{Value: "Loaned", Label: "Loaned", Cls: "status-loaned"},
	},
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	adminUsername = getEnv("LOGAPP_ADMIN_USERNAME", "pekat")
	adminPassword = getEnv("LOGAPP_ADMIN_PASSWORD", "traktor")
	checkHour, _ = strconv.Atoi(getEnv("CHECK_HOUR", "8"))
	port := getEnv("PORT", "8787")

	appBaseDir = findAppBase()
	inventoryPath = filepath.Join(appBaseDir, "app", "data", "inventory.json")
	optionsPath = filepath.Join(appBaseDir, "app", "data", "options.json")
	emailConfigPath = filepath.Join(appBaseDir, "app", "data", "email_config.json")
	backupsDir = filepath.Join(appBaseDir, "app", "data", "backups")
	hourlyBackupsDir = filepath.Join(appBaseDir, "app", "data", "hourly_backups")

	for _, d := range []string{
		filepath.Join(appBaseDir, "app", "data"),
		backupsDir,
		hourlyBackupsDir,
		filepath.Join(appBaseDir, "app", "resources"),
	} {
		os.MkdirAll(d, 0755)
	}

	if _, err := os.Stat(inventoryPath); os.IsNotExist(err) {
		os.WriteFile(inventoryPath, []byte("[]"), 0644)
	}
	if _, err := os.Stat(optionsPath); os.IsNotExist(err) {
		b, _ := json.MarshalIndent(defaultOptions, "", "  ")
		os.WriteFile(optionsPath, b, 0644)
	}
	if _, err := os.Stat(emailConfigPath); os.IsNotExist(err) {
		os.WriteFile(emailConfigPath, []byte("{}"), 0644)
	}

	go runDueDateChecker()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/resources/", handleResources)
	mux.HandleFunc("/favicon.ico", handleFavicon)
	mux.HandleFunc("/api/admin-auth", handleAdminAuth)
	mux.HandleFunc("/api/inventory", handleInventory)
	mux.HandleFunc("/api/lock", handleLock)
	mux.HandleFunc("/api/options", handleOptions)
	mux.HandleFunc("/api/check-due", handleCheckDue)
	mux.HandleFunc("/api/email-config", handleEmailConfig)
	mux.HandleFunc("/api/email-test", handleEmailTest)

	log.Printf("equipment-log listening on :%s (appBase=%s)", port, appBaseDir)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// findAppBase returns the directory that contains the "app/" subdirectory.
// During development (go run), that's the working directory.
// In production (compiled binary), it's the executable's directory.
func findAppBase() string {
	if _, err := os.Stat("app"); err == nil {
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	addr := r.RemoteAddr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[:i]
	}
	return addr
}

func jsonErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": msg})
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// ---------------------------------------------------------------------------
// Inventory I/O
// ---------------------------------------------------------------------------

func readInventory() ([]Item, error) {
	b, err := os.ReadFile(inventoryPath)
	if err != nil {
		return nil, err
	}
	var items []Item
	return items, json.Unmarshal(b, &items)
}

func writeInventory(items []Item) error {
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(inventoryPath, b, 0644); err != nil {
		return err
	}
	doBackup(b)
	doHourlyBackup(b)
	return nil
}

// ---------------------------------------------------------------------------
// Backups
// ---------------------------------------------------------------------------

func doBackup(data []byte) {
	name := fmt.Sprintf("inventory_%s.json", time.Now().Format("20060102_150405"))
	_ = os.WriteFile(filepath.Join(backupsDir, name), data, 0644)
	pruneDir(backupsDir, 100)
}

func doHourlyBackup(data []byte) {
	name := fmt.Sprintf("inventory_hourly_%s.json", time.Now().Format("20060102_15"))
	dst := filepath.Join(hourlyBackupsDir, name)
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		_ = os.WriteFile(dst, data, 0644)
		pruneDir(hourlyBackupsDir, 48)
	}
}

func pruneDir(dir string, max int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	for len(entries) > max {
		_ = os.Remove(filepath.Join(dir, entries[0].Name()))
		entries = entries[1:]
	}
}

// ---------------------------------------------------------------------------
// Lock helpers
// ---------------------------------------------------------------------------

func lockActive() bool {
	return editorLock.Token != "" && time.Now().Unix() < editorLock.ExpiresAt
}

func lockOwned(token string) bool {
	return lockActive() && editorLock.Token == token
}

func lockInfo() map[string]any {
	if !lockActive() {
		return map[string]any{"active": false, "holder": "", "expires_at": 0, "mode": ""}
	}
	return map[string]any{
		"active":     true,
		"holder":     editorLock.Holder,
		"expires_at": editorLock.ExpiresAt,
		"mode":       editorLock.Mode,
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(appBaseDir, "app", "index.html"))
}

func handleResources(w http.ResponseWriter, r *http.Request) {
	sub := strings.TrimPrefix(r.URL.Path, "/resources/")
	// Sanitize: only allow simple filenames (no directory traversal)
	sub = filepath.Base(sub)
	if sub == "." || sub == "" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(appBaseDir, "app", "resources", sub))
}

func handleFavicon(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join(appBaseDir, "app", "resources", "favicon.png"))
}

func handleAdminAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, 400, "bad_request")
		return
	}
	if req.Username == adminUsername && req.Password == adminPassword {
		jsonOK(w, map[string]any{"ok": true})
	} else {
		jsonErr(w, 401, "invalid_credentials")
	}
}

func handleInventory(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		mu.Lock()
		items, err := readInventory()
		info := lockInfo()
		mu.Unlock()
		if err != nil {
			jsonErr(w, 500, "read_error")
			return
		}
		jsonOK(w, map[string]any{"items": items, "lock": info})

	case http.MethodPut:
		var req struct {
			Token string `json:"token"`
			Items []Item `json:"items"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, 400, "bad_request")
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if !lockOwned(req.Token) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(423)
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "lock_required", "lock": lockInfo()})
			return
		}
		if err := writeInventory(req.Items); err != nil {
			jsonErr(w, 500, "write_error")
			return
		}
		jsonOK(w, map[string]any{"ok": true})

	default:
		http.Error(w, "method not allowed", 405)
	}
}

func handleLock(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			Token string `json:"token"`
			Mode  string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, 400, "bad_request")
			return
		}
		ip := clientIP(r)
		mu.Lock()
		defer mu.Unlock()
		if lockActive() && editorLock.Token != req.Token {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(423)
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "locked", "lock": lockInfo()})
			return
		}
		editorLock = editorLockState{
			Holder:    ip,
			Token:     req.Token,
			ExpiresAt: time.Now().Add(15 * time.Minute).Unix(),
			Mode:      req.Mode,
		}
		jsonOK(w, map[string]any{
			"ok":         true,
			"expires_at": editorLock.ExpiresAt,
			"mode":       editorLock.Mode,
			"holder":     editorLock.Holder,
		})

	case http.MethodDelete:
		var req struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, 400, "bad_request")
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if !lockOwned(req.Token) {
			jsonErr(w, 403, "not_owner")
			return
		}
		editorLock = editorLockState{}
		jsonOK(w, map[string]any{"ok": true})

	default:
		http.Error(w, "method not allowed", 405)
	}
}

func handleOptions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		mu.Lock()
		b, err := os.ReadFile(optionsPath)
		mu.Unlock()
		if err != nil {
			jsonErr(w, 500, "read_error")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)

	case http.MethodPut:
		var req struct {
			Token         string            `json:"token"`
			Types         []string          `json:"types"`
			Statuses      []StatusOption    `json:"statuses"`
			TypeRenames   map[string]string `json:"typeRenames"`
			StatusRenames map[string]string `json:"statusRenames"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, 400, "bad_request")
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if !lockOwned(req.Token) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(423)
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "lock_required", "lock": lockInfo()})
			return
		}
		// Sort TYPE options alphabetically (case-insensitive)
		sort.Slice(req.Types, func(i, j int) bool {
			return strings.ToLower(req.Types[i]) < strings.ToLower(req.Types[j])
		})
		opts := Options{Types: req.Types, Statuses: req.Statuses}
		b, _ := json.MarshalIndent(opts, "", "  ")
		if err := os.WriteFile(optionsPath, b, 0644); err != nil {
			jsonErr(w, 500, "write_error")
			return
		}
		// Cascade renames to inventory items
		if len(req.TypeRenames) > 0 || len(req.StatusRenames) > 0 {
			if inv, err := readInventory(); err == nil {
				changed := false
				for i, item := range inv {
					if newType, ok := req.TypeRenames[item.Type]; ok {
						inv[i].Type = newType
						changed = true
					}
					if newStatus, ok := req.StatusRenames[item.Status]; ok {
						inv[i].Status = newStatus
						changed = true
					}
				}
				if changed {
					_ = writeInventory(inv)
				}
			}
		}
		jsonOK(w, map[string]any{"ok": true})

	default:
		http.Error(w, "method not allowed", 405)
	}
}

func handleCheckDue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, 400, "bad_request")
		return
	}
	if req.Username != adminUsername || req.Password != adminPassword {
		jsonErr(w, 401, "invalid_credentials")
		return
	}
	due, sent := checkDueItems()
	jsonOK(w, map[string]any{"due_items": due, "email_sent": sent})
}

// ---------------------------------------------------------------------------
// Email helpers
// ---------------------------------------------------------------------------

// encodeSubject returns an RFC 2047 encoded-word subject so non-ASCII characters
// (e.g. em-dash) render correctly in all email clients including Outlook.
func encodeSubject(s string) string {
	return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
}

// ---------------------------------------------------------------------------
// Email config I/O
// ---------------------------------------------------------------------------

func readEmailConfig() (EmailConfig, error) {
	b, err := os.ReadFile(emailConfigPath)
	if err != nil {
		return EmailConfig{}, err
	}
	var cfg EmailConfig
	return cfg, json.Unmarshal(b, &cfg)
}

func writeEmailConfig(cfg EmailConfig) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(emailConfigPath, b, 0644)
}

// ---------------------------------------------------------------------------
// Due-date checker
// ---------------------------------------------------------------------------

func runDueDateChecker() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		if time.Now().Hour() == checkHour {
			due, sent := checkDueItems()
			log.Printf("due-date check: %d due item(s), email_sent=%v", len(due), sent)
		}
	}
}

func checkDueItems() ([]Item, bool) {
	mu.Lock()
	items, err := readInventory()
	mu.Unlock()
	if err != nil {
		log.Printf("due-date check: read error: %v", err)
		return nil, false
	}
	today := time.Now().Format("2006-01-02")
	var due []Item
	for _, item := range items {
		if item.SentDate == "" || item.ReturnDate == "" {
			continue
		}
		if item.ReturnDate <= today {
			due = append(due, item)
		}
	}
	if len(due) == 0 {
		return due, false
	}
	cfg, err := readEmailConfig()
	if err != nil || cfg.SMTPHost == "" || cfg.SMTPUser == "" || cfg.SMTPPass == "" || cfg.SMTPFrom == "" || cfg.SMTPTo == "" {
		log.Printf("due-date check: email not configured, skipping")
		return due, false
	}
	if err := sendDueEmail(due, cfg); err != nil {
		log.Printf("due-date check: smtp error: %v", err)
		return due, false
	}
	return due, true
}

func sendDueEmail(items []Item, cfg EmailConfig) error {
	var ccList []string
	for _, addr := range strings.Split(cfg.SMTPCC, ",") {
		if addr = strings.TrimSpace(addr); addr != "" {
			ccList = append(ccList, addr)
		}
	}

	var sb strings.Builder
	subject := encodeSubject(fmt.Sprintf("Equipment Log: %d item(s) due/overdue", len(items)))
	sb.WriteString(fmt.Sprintf("From: %s\r\n", cfg.SMTPFrom))
	sb.WriteString(fmt.Sprintf("To: %s\r\n", cfg.SMTPTo))
	if len(ccList) > 0 {
		sb.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(ccList, ", ")))
	}
	sb.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	sb.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().Format(time.RFC1123Z)))
	sb.WriteString(fmt.Sprintf("Message-ID: <%d@logapp>\r\n", time.Now().UnixNano()))
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	sb.WriteString("\r\n")
	sb.WriteString("The following equipment items are due or overdue for return:\r\n\r\n")
	for _, it := range items {
		note := it.Note
		if note == "" {
			note = "\u2014"
		}
		sb.WriteString(fmt.Sprintf("  %s | %s | Status: %s | Sent: %s | Return: %s | Note: %s\r\n",
			it.ID, it.Name, it.Status, it.SentDate, it.ReturnDate, note))
	}
	allRecipients := append([]string{cfg.SMTPTo}, ccList...)
	port := cfg.SMTPPort
	if port == "" {
		port = "587"
	}
	auth := smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPHost)
	return smtp.SendMail(
		fmt.Sprintf("%s:%s", cfg.SMTPHost, port),
		auth,
		cfg.SMTPFrom,
		allRecipients,
		[]byte(sb.String()),
	)
}

// ---------------------------------------------------------------------------
// Email config handlers
// ---------------------------------------------------------------------------

const maskedPass = "\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022"

func handleEmailConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		// Fetch current config — requires admin credentials
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, 400, "bad_request")
			return
		}
		if req.Username != adminUsername || req.Password != adminPassword {
			jsonErr(w, 401, "invalid_credentials")
			return
		}
		cfg, err := readEmailConfig()
		if err != nil {
			jsonErr(w, 500, "read_error")
			return
		}
		// Mask passwords before returning to client
		masked := cfg
		if masked.SMTPPass != "" {
			masked.SMTPPass = maskedPass
		}
		jsonOK(w, map[string]any{"ok": true, "config": masked})

	case http.MethodPut:
		var req struct {
			Username string      `json:"username"`
			Password string      `json:"password"`
			Token    string      `json:"token"`
			Config   EmailConfig `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, 400, "bad_request")
			return
		}
		if req.Username != adminUsername || req.Password != adminPassword {
			jsonErr(w, 401, "invalid_credentials")
			return
		}
		mu.Lock()
		owned := lockOwned(req.Token)
		mu.Unlock()
		if !owned {
			jsonErr(w, 423, "lock_required")
			return
		}
		// Preserve masked or empty passwords from stored config
		existing, existErr := readEmailConfig()
		if req.Config.SMTPPass == maskedPass || req.Config.SMTPPass == "" {
			if existErr == nil {
				req.Config.SMTPPass = existing.SMTPPass
			}
		}
		if err := writeEmailConfig(req.Config); err != nil {
			jsonErr(w, 500, "write_error")
			return
		}
		jsonOK(w, map[string]any{"ok": true})

	default:
		http.Error(w, "method not allowed", 405)
	}
}

func handleEmailTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, 400, "bad_request")
		return
	}
	if req.Username != adminUsername || req.Password != adminPassword {
		jsonErr(w, 401, "invalid_credentials")
		return
	}
	cfg, err := readEmailConfig()
	if err != nil || cfg.SMTPHost == "" || cfg.SMTPUser == "" || cfg.SMTPPass == "" || cfg.SMTPFrom == "" || cfg.SMTPTo == "" {
		jsonErr(w, 422, "email_not_configured")
		return
	}
	fakeItems := []Item{{
		ID: "PV-TEST", Name: "Test Item", Status: "Customer",
		SentDate: time.Now().Format("2006-01-02"),
		ReturnDate: time.Now().Format("2006-01-02"),
		Note: "This is a test email from Equipment Log",
	}}

	var ccList []string
	for _, addr := range strings.Split(cfg.SMTPCC, ",") {
		if addr = strings.TrimSpace(addr); addr != "" {
			ccList = append(ccList, addr)
		}
	}
	var sb strings.Builder
	subject := encodeSubject("Equipment Log \u2014 Test Alert")
	sb.WriteString(fmt.Sprintf("From: %s\r\n", cfg.SMTPFrom))
	sb.WriteString(fmt.Sprintf("To: %s\r\n", cfg.SMTPTo))
	if len(ccList) > 0 {
		sb.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(ccList, ", ")))
	}
	sb.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	sb.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().Format(time.RFC1123Z)))
	sb.WriteString(fmt.Sprintf("Message-ID: <%d@logapp>\r\n", time.Now().UnixNano()))
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	sb.WriteString("\r\n")
	sb.WriteString("This is a test email from Equipment Log.\r\n\r\nSample entry:\r\n\r\n")
	for _, it := range fakeItems {
		note := it.Note
		if note == "" {
			note = "\u2014"
		}
		sb.WriteString(fmt.Sprintf("  %s | %s | Status: %s | Sent: %s | Return: %s | Note: %s\r\n",
			it.ID, it.Name, it.Status, it.SentDate, it.ReturnDate, note))
	}
	allRecipients := append([]string{cfg.SMTPTo}, ccList...)
	port := cfg.SMTPPort
	if port == "" {
		port = "587"
	}
	auth := smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPHost)
	if err := smtp.SendMail(
		fmt.Sprintf("%s:%s", cfg.SMTPHost, port),
		auth, cfg.SMTPFrom, allRecipients, []byte(sb.String()),
	); err != nil {
		jsonErr(w, 500, err.Error())
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}
