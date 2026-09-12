package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed web/templates/*.html
var templateFS embed.FS

//go:embed web/vendor/htmx.min.js
var htmxFS embed.FS

//go:embed web/static/style.css web/static/app.js
var staticFS embed.FS

var reviewTmpl *template.Template

func initTemplates() error {
	if reviewTmpl != nil {
		return nil
	}
	funcs := template.FuncMap{
		"pct1": func(v float64) string { return fmt.Sprintf("%.1f", v*100) },
		"pct0": func(v float64) string { return fmt.Sprintf("%.0f", v*100) },
	}
	t, err := template.New("omniviz").Funcs(funcs).ParseFS(templateFS, "web/templates/*.html")
	if err != nil {
		return err
	}
	reviewTmpl = t
	return nil
}

type chipView struct {
	Status string
	Label  string
	Count  int
}

type headerView struct {
	ProjectName  string
	Commit       string
	GodotVersion string
	GeneratedAt  string
	Chips        []chipView
	Approvable   int
}

type shotRow struct {
	Key    string
	KeyURL string
	Status string
	Badge  string
}

type jobView struct {
	ID    string
	Shots []shotRow
}

type listData struct {
	Jobs []jobView
}

type refreshListData struct {
	List   listData
	Header headerView
}

type shotDetail struct {
	Key            string
	KeyURL         string
	Job            string
	Scene          string
	Status         string
	Mode           string
	Dims           string
	ThresholdPct   string
	MaxChangedPct  string
	Recording      bool
	Frames         int
	HasBaseline    bool
	HasCurrent     bool
	HasDiff        bool
	CanApprove     bool
	MismatchPct    string
	MismatchPixels int
	ChangedPct     string
	ChangedPixels  int
	Error          string
	Log            string
}

type pageData struct {
	Header headerView
	List   listData
	Detail *shotDetail
	Frames *framesView
}

// framesView backs the recording player (modal content).
type framesView struct {
	Job   string
	Count int
	Max   int
}

func keyURL(key string) string {
	return strings.ReplaceAll(key, "/", "--")
}

type server struct {
	c *runCtx
}

func (s *server) report() *Report {
	r, err := loadReport(filepath.Join(s.c.output, "report.json"))
	if err != nil {
		return &Report{}
	}
	return r
}

var chipOrder = []struct{ status, label string }{
	{"fail", "failing"},
	{"size", "size changed"},
	{"error", "errors"},
	{"new", "new"},
	{"pass", "passing"},
}

func buildHeader(s *server, r *Report) headerView {
	h := headerView{
		ProjectName:  filepath.Base(s.c.project),
		Commit:       r.Commit,
		GodotVersion: r.GodotVersion,
	}
	if r.GeneratedAt != "" {
		if t, err := time.Parse(time.RFC3339, r.GeneratedAt); err == nil {
			h.GeneratedAt = t.Format("15:04:05")
		} else {
			h.GeneratedAt = r.GeneratedAt
		}
	}
	for _, co := range chipOrder {
		if n := r.Summary[co.status]; n > 0 {
			h.Chips = append(h.Chips, chipView{Status: co.status, Label: co.label, Count: n})
		}
	}
	h.Approvable = r.Summary["fail"] + r.Summary["new"]
	return h
}

func buildList(r *Report) listData {
	var jobs []jobView
	byID := map[string]*jobView{}
	for _, shot := range r.Shots {
		jv := byID[shot.Job]
		if jv == nil {
			jobs = append(jobs, jobView{ID: shot.Job})
			jv = &jobs[len(jobs)-1]
			byID[shot.Job] = jv
		}
		row := shotRow{Key: shot.Key, KeyURL: keyURL(shot.Key), Status: shot.Status, Badge: failBadge(shot)}
		jv.Shots = append(jv.Shots, row)
	}
	return listData{Jobs: jobs}
}

func failBadge(shot ShotResult) string {
	switch shot.Status {
	case "fail":
		return pct1(shot.ChangedRatio) + "%"
	case "new":
		return "new"
	case "size", "error", "missing":
		return shot.Status
	}
	return ""
}

func pct1(v float64) string { return fmt.Sprintf("%.1f", v*100) }

func buildDetail(s *server, r *Report, key string) *shotDetail {
	for _, shot := range r.Shots {
		if shot.Key != key {
			continue
		}
		d := &shotDetail{
			Key:            shot.Key,
			KeyURL:         keyURL(shot.Key),
			Job:            shot.Job,
			Scene:          shot.Scene,
			Status:         shot.Status,
			ThresholdPct:   pct1(shot.Threshold) + "%",
			MaxChangedPct:  pct1(shot.MaxChanged),
			Recording:      shot.Recording,
			Frames:         shot.Frames,
			MismatchPct:    pct1(shot.MismatchRatio),
			MismatchPixels: shot.MismatchPixels,
			ChangedPct:     pct1(shot.ChangedRatio),
			ChangedPixels:  shot.ChangedPixels,
			Error:          shot.Error,
			Log:            shot.Log,
		}
		if shot.Width > 0 {
			d.Dims = fmt.Sprintf("%dx%d", shot.Width, shot.Height)
		}
		d.HasBaseline = fileExists(filepath.Join(s.c.baselines, filepath.FromSlash(key)+".png"))
		d.HasCurrent = fileExists(filepath.Join(s.c.output, "current", key+".png"))
		d.HasDiff = fileExists(filepath.Join(s.c.output, "diff", key+".png"))
		d.CanApprove = d.HasCurrent && (shot.Status == "fail" || shot.Status == "new" || shot.Status == "size")
		return d
	}
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func render(w http.ResponseWriter, name string, data any) []byte {
	var buf bytes.Buffer
	if err := reviewTmpl.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return nil
	}
	return buf.Bytes()
}

func (s *server) handlePage(w http.ResponseWriter, r *http.Request, detailKey string) {
	rep := s.report()
	data := pageData{Header: buildHeader(s, rep), List: buildList(rep)}
	if detailKey != "" {
		data.Detail = buildDetail(s, rep, detailKey)
		if data.Detail != nil {
			data.Detail.Mode = r.URL.Query().Get("mode")
			if data.Detail.Mode == "" {
				if data.Detail.HasBaseline {
					data.Detail.Mode = "overlay"
				} else {
					data.Detail.Mode = "current"
				}
			}
		}
	}
	if r.URL.Query().Get("frames") != "" && data.Detail != nil {
		data.Frames = buildFramesView(s, data.Detail.Job)
	}
	body := render(w, "page", data)
	if body == nil {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(body)
}

func buildFramesView(s *server, job string) *framesView {
	frames := listFrames(filepath.Join(s.c.output, "frames", job))
	if len(frames) == 0 {
		return nil
	}
	return &framesView{Job: job, Count: len(frames), Max: len(frames) - 1}
}

func (s *server) handleListRefresh(w http.ResponseWriter, r *http.Request) {
	rep := s.report()
	data := refreshListData{List: buildList(rep), Header: buildHeader(s, rep)}
	body := render(w, "refresh_list", data)
	if body == nil {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(body)
}

func (s *server) handleShotFragment(w http.ResponseWriter, key string) {
	rep := s.report()
	d := buildDetail(s, rep, key)
	if d == nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`<div class="empty">shot ` + template.HTMLEscapeString(key) + ` is not in the current report — run <code>omniviz test</code></div>`))
		return
	}
	body := render(w, "shot", d)
	if body == nil {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(body)
}

func (s *server) handleApprove(w http.ResponseWriter, key string) {
	approved, err := approveKeys(s.c, []string{key})
	if err != nil || len(approved) == 0 {
		msg := fmt.Sprintf("approve failed: %v", err)
		http.Error(w, msg, http.StatusUnprocessableEntity)
		return
	}
	rep := s.report()
	d := buildDetail(s, rep, key)
	if d == nil {
		http.Error(w, "shot vanished", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := reviewTmpl.ExecuteTemplate(&buf, "shot", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	row := shotRowFrom(rep, key)
	if err := reviewTmpl.ExecuteTemplate(&buf, "row_partial", row); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := reviewTmpl.ExecuteTemplate(&buf, "header_partial", buildHeader(s, rep)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}

func shotRowFrom(r *Report, key string) shotRow {
	for _, shot := range r.Shots {
		if shot.Key == key {
			row := shotRow{Key: shot.Key, KeyURL: keyURL(shot.Key), Status: shot.Status}
			row.Badge = failBadge(shot)
			return row
		}
	}
	return shotRow{Key: key, KeyURL: keyURL(key), Status: "pass"}
}

func (s *server) handleApproveAll(w http.ResponseWriter, r *http.Request) {
	keys, err := pendingKeys(s.c)
	if err == nil && len(keys) > 0 {
		approveKeys(s.c, keys)
	}
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleFramesFragment(w http.ResponseWriter, job string) {
	dir := filepath.Join(s.c.output, "frames", job)
	frames := listFrames(dir)
	if len(frames) == 0 {
		http.Error(w, "no recording frames found for "+job, http.StatusNotFound)
		return
	}
	data := struct {
		Job   string
		Count int
		Max   int
	}{Job: job, Count: len(frames), Max: len(frames) - 1}
	body := render(w, "frames", data)
	if body == nil {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(body)
}

func listFrames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".png") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

func (s *server) handleImg(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	key := strings.TrimSuffix(r.PathValue("key"), ".png")
	var dir string
	switch kind {
	case "baseline":
		dir = s.c.baselines
	case "current":
		dir = filepath.Join(s.c.output, "current")
	case "diff":
		dir = filepath.Join(s.c.output, "diff")
	default:
		http.NotFound(w, r)
		return
	}
	p, ok := safeJoin(dir, key+".png")
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, p)
}

func (s *server) handleFrame(w http.ResponseWriter, r *http.Request) {
	job := r.PathValue("job")
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 0 {
		http.NotFound(w, r)
		return
	}
	frames := listFrames(filepath.Join(s.c.output, "frames", job))
	if n >= len(frames) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "max-age=3600")
	http.ServeFile(w, r, frames[n])
}

func safeJoin(dir, name string) (string, bool) {
	clean := path.Clean("/" + filepath.ToSlash(name))
	p := filepath.Join(dir, filepath.FromSlash(clean))
	if !strings.HasPrefix(p, filepath.Clean(dir)+string(os.PathSeparator)) && filepath.Clean(p) != filepath.Clean(dir) {
		return "", false
	}
	if !fileExists(p) {
		return "", false
	}
	return p, true
}

var staticTypes = map[string]string{
	"style.css":   "text/css; charset=utf-8",
	"app.js":      "text/javascript; charset=utf-8",
	"htmx.min.js": "text/javascript; charset=utf-8",
}

func (s *server) handleStatic(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	ct, ok := staticTypes[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	var data []byte
	var err error
	if name == "htmx.min.js" {
		data, err = htmxFS.ReadFile("web/vendor/htmx.min.js")
	} else {
		data, err = staticFS.ReadFile("web/static/" + name)
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

func (s *server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { s.handlePage(w, r, "") })
	mux.HandleFunc("GET /shot/{key...}", func(w http.ResponseWriter, r *http.Request) {
		s.handlePage(w, r, r.PathValue("key"))
	})
	mux.HandleFunc("GET /frag/list", s.handleListRefresh)
	mux.HandleFunc("GET /frag/shot/{key...}", func(w http.ResponseWriter, r *http.Request) {
		s.handleShotFragment(w, r.PathValue("key"))
	})
	mux.HandleFunc("POST /api/approve/{key...}", func(w http.ResponseWriter, r *http.Request) {
		s.handleApprove(w, r.PathValue("key"))
	})
	mux.HandleFunc("POST /api/approve-all", s.handleApproveAll)
	mux.HandleFunc("GET /frag/frames/{job}", func(w http.ResponseWriter, r *http.Request) {
		s.handleFramesFragment(w, r.PathValue("job"))
	})
	mux.HandleFunc("GET /img/{kind}/{key...}", s.handleImg)
	mux.HandleFunc("GET /frame/{job}/{n}", s.handleFrame)
	mux.HandleFunc("GET /static/{file}", s.handleStatic)
	mux.HandleFunc("GET /api/run", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.report())
	})
	return mux
}

func cmdReview(argv []string) error {
	fs := flag.NewFlagSet("review", flag.ExitOnError)
	project := fs.String("project", ".", "project root containing omniviz.toml")
	port := fs.Int("port", 8420, "port to listen on")
	noOpen := fs.Bool("no-open", false, "do not open the browser")
	fs.Parse(argv)
	c, err := loadCtx(*project)
	if err != nil {
		return err
	}
	if err := initTemplates(); err != nil {
		return err
	}
	s := &server{c: c}
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	url := fmt.Sprintf("http://%s", addr)
	fmt.Printf("omniviz review: %s  (ctrl-c to stop)\n", url)
	if !*noOpen {
		go func() {
			time.Sleep(300 * time.Millisecond)
			exec.Command("xdg-open", url).Start()
		}()
	}
	srv := &http.Server{Addr: addr, Handler: s.routes()}
	return srv.ListenAndServe()
}
