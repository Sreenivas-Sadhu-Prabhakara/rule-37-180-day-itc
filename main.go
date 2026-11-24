// Command rule-37-180-day-itc watches the 180-day payment condition that input
// tax credit is subject to, and works out what must be reversed and when.
//
// It serves a small web interface on 127.0.0.1 and keeps everything in one
// local JSON file. It makes no outbound connections of any kind: nothing is
// uploaded, nothing is filed, and no supplier is ever contacted. It prepares
// figures and a working paper; you file the return.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sreenivas-sadhu-prabhakara/rule-37-180-day-itc/internal/rule37"
)

//go:embed web
var webFiles embed.FS

const (
	// maxBodyBytes caps a JSON request body. The forms send a few hundred
	// bytes; anything larger is a mistake, and reading it unbounded would be
	// one too.
	maxBodyBytes = 1 << 20

	// maxUploadBytes caps a register upload. A purchase register for a year of
	// a small business is a few megabytes at most.
	maxUploadBytes = 64 << 20
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "address to listen on; keep it on 127.0.0.1 so the tool stays on this machine")
	data := flag.String("data", "./rule-37-itc-data.json", "path to the local data file")
	flag.Parse()

	st, err := openStore(*data)
	if err != nil {
		log.Fatalf("rule 37 guard: %v", err)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           newServer(st).routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("Rule 37 — 180-day input tax credit reversal guard")
	log.Printf("open http://%s/ in your browser", *addr)
	log.Printf("data file: %s (nothing leaves this machine)", *data)
	log.Printf("press Ctrl+C to stop")

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("rule 37 guard: %v", err)
	}
}

// server wires the JSON API and the embedded interface over one store.
type server struct {
	store *store
	// today is injectable so the handlers can be tested on a fixed date.
	today func() rule37.Day
}

func newServer(st *store) *server {
	return &server{
		store: st,
		today: func() rule37.Day { return rule37.DayOf(time.Now()) },
	}
}

func (s *server) routes() http.Handler {
	ui, err := fs.Sub(webFiles, "web")
	if err != nil {
		// The directory is embedded at build time; this cannot fail at runtime.
		panic(fmt.Sprintf("embedded interface missing: %v", err))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("GET /api/reference", s.handleReference)
	mux.HandleFunc("POST /api/settings", s.handleSettings)
	mux.HandleFunc("POST /api/invoices", s.handleCreateInvoice)
	mux.HandleFunc("PUT /api/invoices/{id}", s.handleUpdateInvoice)
	mux.HandleFunc("DELETE /api/invoices/{id}", s.handleDeleteInvoice)
	mux.HandleFunc("DELETE /api/invoices", s.handleClearInvoices)
	mux.HandleFunc("POST /api/invoices/{id}/payments", s.handleAddPayment)
	mux.HandleFunc("DELETE /api/invoices/{id}/payments/{index}", s.handleDeletePayment)
	mux.HandleFunc("POST /api/import", s.handleImport)
	mux.HandleFunc("GET /api/export", s.handleExport)
	mux.HandleFunc("GET /api/workingpaper", s.handleWorkingPaper)
	mux.Handle("GET /", http.FileServerFS(ui))
	return headers(mux)
}

// headers adds the few response headers a local tool should still set.
func headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

/* ---------------------------------------------------------------- the state */

// stateResponse is what the interface renders: the stored ledger plus
// everything the domain computes from it on the chosen date.
type stateResponse struct {
	Business rule37.Business `json:"business"`
	Settings rule37.Settings `json:"settings"`
	Defaults rule37.Settings `json:"defaults"`
	AsOf     rule37.Day      `json:"asOf"`
	Today    rule37.Day      `json:"today"`
	Updated  string          `json:"updated,omitempty"`

	Lines   []rule37.Line  `json:"lines"`
	Summary rule37.Summary `json:"summary"`
	Plan    rule37.Plan    `json:"plan"`
	Budget  int64          `json:"budget"`
	Views   []rule37.View  `json:"views"`
}

func (s *server) state(asOf rule37.Day, budget int64) stateResponse {
	data := s.store.snapshot()
	set := data.Settings.Normalise()
	lines := rule37.AssessAll(data.Invoices, asOf, set)
	return stateResponse{
		Business: data.Business,
		Settings: set,
		Defaults: rule37.DefaultSettings(),
		AsOf:     asOf,
		Today:    s.today(),
		Updated:  data.Updated,
		Lines:    lines,
		Summary:  rule37.Totals(lines, asOf, set),
		Plan:     rule37.PayFirst(lines, asOf, set, budget),
		Budget:   budget,
		Views:    rule37.Views,
	}
}

func (s *server) handleState(w http.ResponseWriter, r *http.Request) {
	asOf, budget, err := s.readQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, s.state(asOf, budget))
}

// referenceResponse is the law this tool implements, in the form the interface
// shows it: every figure with the provision it comes from, so a number on the
// screen can always be traced to its authority.
type referenceResponse struct {
	WindowDays             int      `json:"windowDays"`
	ReversalOffsetPeriods  int      `json:"reversalOffsetPeriods"`
	InterestRateDefaultPct float64  `json:"interestRateDefaultPct"`
	InterestCeilingPct     float64  `json:"interestCeilingPct"`
	InterestYearDays       int      `json:"interestYearDays"`
	MonthlyDueDay          int      `json:"monthlyDueDay"`
	QuarterlyDueDayEarly   int      `json:"quarterlyDueDayEarly"`
	QuarterlyDueDayLate    int      `json:"quarterlyDueDayLate"`
	StatesEarly            []string `json:"statesEarly"`
	StatesLate             []string `json:"statesLate"`
	TableReversal          string   `json:"tableReversal"`
	TableReclaim           string   `json:"tableReclaim"`
	TableReclaimDisclosure string   `json:"tableReclaimDisclosure"`
	ReversalLedgerName     string   `json:"reversalLedgerName"`
	BucketEdges            []int    `json:"bucketEdges"`
}

func (s *server) handleReference(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, referenceResponse{
		WindowDays:             rule37.PaymentWindowDays,
		ReversalOffsetPeriods:  rule37.ReversalReturnOffsetPeriods,
		InterestRateDefaultPct: rule37.InterestRateAnnualPctDefault,
		InterestCeilingPct:     rule37.SectionFiftyCeilingPct,
		InterestYearDays:       rule37.DaysInYearForInterest,
		MonthlyDueDay:          rule37.MonthlyDueDay,
		QuarterlyDueDayEarly:   rule37.QuarterlyDueDayEarly,
		QuarterlyDueDayLate:    rule37.QuarterlyDueDayLate,
		StatesEarly:            rule37.StatesByQuarterlyDueDay(rule37.QuarterlyDueDayEarly),
		StatesLate:             rule37.StatesByQuarterlyDueDay(rule37.QuarterlyDueDayLate),
		TableReversal:          rule37.TableReversal,
		TableReclaim:           rule37.TableReclaim,
		TableReclaimDisclosure: rule37.TableReclaimDisclosure,
		ReversalLedgerName:     rule37.ReversalLedgerName,
		BucketEdges:            rule37.DefaultBucketEdges,
	})
}

/* ------------------------------------------------------------- the settings */

type settingsRequest struct {
	Business rule37.Business `json:"business"`
	Settings rule37.Settings `json:"settings"`
}

func (s *server) handleSettings(w http.ResponseWriter, r *http.Request) {
	var in settingsRequest
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	in.Business.Name = strings.TrimSpace(in.Business.Name)
	in.Business.GSTIN = rule37.NormaliseGSTIN(in.Business.GSTIN)
	if in.Business.GSTIN != "" && !rule37.ValidGSTINFormat(in.Business.GSTIN) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("that is not a 15-character GSTIN — leave it blank if you would rather not enter it"))
		return
	}
	set := in.Settings.Normalise()
	if err := set.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if _, err := s.store.update(func(l *ledger) error {
		l.Business = in.Business
		l.Settings = set
		return nil
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeState(w, r, http.StatusOK)
}

/* ------------------------------------------------------------- the invoices */

var errNotFound = errors.New("not found")

func (s *server) handleCreateInvoice(w http.ResponseWriter, r *http.Request) {
	var in rule37.Invoice
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	in = tidy(in)
	if err := in.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	in.ID = newID()

	if _, err := s.store.update(func(l *ledger) error {
		l.Invoices = append(l.Invoices, in)
		return nil
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeState(w, r, http.StatusCreated)
}

func (s *server) handleUpdateInvoice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in rule37.Invoice
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	in = tidy(in)
	if err := in.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	in.ID = id

	_, err := s.store.update(func(l *ledger) error {
		for i := range l.Invoices {
			if l.Invoices[i].ID == id {
				// The payments already recorded against the invoice are kept
				// unless the request carries its own, so editing a figure on
				// the form cannot quietly erase a payment history.
				if in.Payments == nil {
					in.Payments = l.Invoices[i].Payments
				}
				l.Invoices[i] = in
				return nil
			}
		}
		return errNotFound
	})
	if s.failed(w, err) {
		return
	}
	s.writeState(w, r, http.StatusOK)
}

func (s *server) handleDeleteInvoice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, err := s.store.update(func(l *ledger) error {
		for i := range l.Invoices {
			if l.Invoices[i].ID == id {
				l.Invoices = append(l.Invoices[:i], l.Invoices[i+1:]...)
				return nil
			}
		}
		return errNotFound
	})
	if s.failed(w, err) {
		return
	}
	s.writeState(w, r, http.StatusOK)
}

func (s *server) handleClearInvoices(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.update(func(l *ledger) error {
		l.Invoices = nil
		return nil
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeState(w, r, http.StatusOK)
}

/* ------------------------------------------------------------- the payments */

func (s *server) handleAddPayment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in rule37.Payment
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	in.Ref = strings.TrimSpace(in.Ref)
	in.Line = 0
	if in.Amount <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("enter the amount paid to the supplier"))
		return
	}

	_, err := s.store.update(func(l *ledger) error {
		for i := range l.Invoices {
			if l.Invoices[i].ID == id {
				l.Invoices[i].Payments = append(l.Invoices[i].Payments, in)
				l.Invoices[i].SortPayments()
				return nil
			}
		}
		return errNotFound
	})
	if s.failed(w, err) {
		return
	}
	s.writeState(w, r, http.StatusCreated)
}

func (s *server) handleDeletePayment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || index < 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("which payment?"))
		return
	}
	_, err = s.store.update(func(l *ledger) error {
		for i := range l.Invoices {
			if l.Invoices[i].ID != id {
				continue
			}
			if index >= len(l.Invoices[i].Payments) {
				return errNotFound
			}
			l.Invoices[i].Payments = append(l.Invoices[i].Payments[:index], l.Invoices[i].Payments[index+1:]...)
			return nil
		}
		return errNotFound
	})
	if s.failed(w, err) {
		return
	}
	s.writeState(w, r, http.StatusOK)
}

/* --------------------------------------------------------------- the import */

// importResponse reports what came out of the files: how much was read, and
// every row that could not be used, with its line number.
type importResponse struct {
	Register rule37.Import        `json:"register"`
	Payments rule37.PaymentImport `json:"payments"`
	Attached []rule37.RowProblem  `json:"attached,omitempty"`
	Imported int                  `json:"imported"`
	Replaced bool                 `json:"replaced"`
	State    stateResponse        `json:"state"`
}

// importPaths lets the user point at files already on this computer instead of
// uploading them, which is less work and keeps the file where it is.
type importPaths struct {
	RegisterPath string `json:"registerPath"`
	PaymentsPath string `json:"paymentsPath"`
	Replace      bool   `json:"replace"`
}

func (s *server) handleImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	asOf, budget, err := s.readQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	var (
		registerBytes, paymentsBytes []byte
		replace                      bool
	)
	switch ct := r.Header.Get("Content-Type"); {
	case strings.HasPrefix(ct, "multipart/form-data"):
		if err := r.ParseMultipartForm(16 << 20); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("could not read the upload: %w", err))
			return
		}
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()
		replace = r.FormValue("replace") == "true"
		registerBytes, err = formFile(r.MultipartForm, "register")
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		paymentsBytes, err = formFile(r.MultipartForm, "payments")
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	default:
		var in importPaths
		if err := readJSONLimited(r, maxBodyBytes, &in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		replace = in.Replace
		if registerBytes, err = readLocalFile(in.RegisterPath, "purchase register"); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if paymentsBytes, err = readLocalFile(in.PaymentsPath, "payments file"); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}

	if len(registerBytes) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("choose a purchase register to read, or give the path to one on this computer"))
		return
	}

	out := importResponse{Replaced: replace}
	out.Register, err = rule37.ParseRegisterCSV(bytesReader(registerBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	invoices := out.Register.Invoices

	if len(paymentsBytes) > 0 {
		out.Payments, err = rule37.ParsePaymentsCSV(bytesReader(paymentsBytes))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		invoices, out.Attached = rule37.AttachPayments(invoices, out.Payments.Rows)
	}

	for i := range invoices {
		invoices[i].ID = newID()
	}
	out.Imported = len(invoices)

	if _, err := s.store.update(func(l *ledger) error {
		if replace {
			l.Invoices = invoices
			return nil
		}
		l.Invoices = append(l.Invoices, invoices...)
		return nil
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out.State = s.state(asOf, budget)
	writeJSON(w, http.StatusOK, out)
}

/* ---------------------------------------------------------- the two outputs */

func (s *server) handleExport(w http.ResponseWriter, r *http.Request) {
	asOf, budget, err := s.readQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	view := rule37.View(strings.TrimSpace(r.URL.Query().Get("view")))
	if view == "" {
		view = rule37.ViewWatchlist
	}
	if !view.Valid() {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%q is not a list this tool can export", view))
		return
	}

	st := s.state(asOf, budget)
	raw, err := rule37.CSV(view, st.Lines, st.Plan, asOf, st.Settings)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	name := fmt.Sprintf("rule-37-%s-%s.csv", view, asOf)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(name))
	_, _ = w.Write(raw)
}

func (s *server) handleWorkingPaper(w http.ResponseWriter, r *http.Request) {
	asOf, budget, err := s.readQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	st := s.state(asOf, budget)

	period := st.Summary.ThisPeriod
	if raw := strings.TrimSpace(r.URL.Query().Get("period")); raw != "" {
		day, err := rule37.ParseDay(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("which return period? give any date inside it as YYYY-MM-DD"))
			return
		}
		period = rule37.PeriodOf(day, st.Settings.ReturnPeriodMonths)
	}

	data := s.store.snapshot()
	text := rule37.WorkingPaper(data.Business, period, st.Lines, asOf, st.Settings)
	if r.URL.Query().Get("download") == "true" {
		name := fmt.Sprintf("rule-37-working-paper-%s.txt", period.Code())
		w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(name))
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, text)
}

/* --------------------------------------------------------------- the plumbing */

// readQuery reads the date the figures are computed for and the cash available
// for the payment plan.
func (s *server) readQuery(r *http.Request) (rule37.Day, int64, error) {
	q := r.URL.Query()
	asOf, err := rule37.ParseDay(q.Get("asOf"))
	if err != nil {
		return rule37.Day{}, 0, err
	}
	if asOf.IsZero() {
		asOf = s.today()
	}
	budget := rule37.NoBudgetLimit
	if raw := strings.TrimSpace(q.Get("budget")); raw != "" {
		budget, err = rule37.ParseAmount(raw)
		if err != nil {
			return rule37.Day{}, 0, fmt.Errorf("the cash available: %w", err)
		}
		if budget < 0 {
			budget = rule37.NoBudgetLimit
		}
	}
	return asOf, budget, nil
}

func (s *server) writeState(w http.ResponseWriter, r *http.Request, code int) {
	asOf, budget, err := s.readQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, code, s.state(asOf, budget))
}

// failed turns a store error into a response, and reports whether it did.
func (s *server) failed(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, errNotFound):
		writeError(w, http.StatusNotFound, fmt.Errorf("no such record — the list may have changed since the page was loaded"))
		return true
	case err != nil:
		writeError(w, http.StatusInternalServerError, err)
		return true
	}
	return false
}

// tidy trims and normalises what came in from a form before it is validated or
// stored.
func tidy(in rule37.Invoice) rule37.Invoice {
	in.SupplierName = strings.TrimSpace(in.SupplierName)
	in.SupplierGSTIN = rule37.NormaliseGSTIN(in.SupplierGSTIN)
	in.InvoiceNo = strings.TrimSpace(in.InvoiceNo)
	in.Note = strings.TrimSpace(in.Note)
	in.Line = 0
	for i := range in.Payments {
		in.Payments[i].Ref = strings.TrimSpace(in.Payments[i].Ref)
		in.Payments[i].Line = 0
	}
	return in
}

// formFile reads one uploaded file, or nothing when the field was left empty.
func formFile(form *multipart.Form, field string) ([]byte, error) {
	if form == nil {
		return nil, nil
	}
	files := form.File[field]
	if len(files) == 0 {
		return nil, nil
	}
	f, err := files[0].Open()
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", files[0].Filename, err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxUploadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", files[0].Filename, err)
	}
	if len(raw) > maxUploadBytes {
		return nil, fmt.Errorf("%s is larger than %d MB", files[0].Filename, maxUploadBytes>>20)
	}
	return raw, nil
}

// readLocalFile reads a file the user named by path, expanding a leading ~.
// This is the user's own machine and their own file; the path is taken as
// given, and a directory or an unreadable file is reported plainly.
func readLocalFile(path, what string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
		}
	}
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("there is no %s at %s", what, path)
	case err != nil:
		return nil, fmt.Errorf("could not read the %s at %s: %w", what, path, err)
	case info.IsDir():
		return nil, fmt.Errorf("%s is a folder, not a %s", path, what)
	case info.Size() > maxUploadBytes:
		return nil, fmt.Errorf("%s is larger than %d MB", path, maxUploadBytes>>20)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read the %s at %s: %w", what, path, err)
	}
	return raw, nil
}

func bytesReader(b []byte) io.Reader { return strings.NewReader(string(b)) }

func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	return readJSONLimited(r, maxBodyBytes, dst)
}

func readJSONLimited(r *http.Request, limit int64, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("could not read the form: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("rule 37 guard: writing response: %v", err)
	}
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
