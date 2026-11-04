package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/apognu/gocal"
	"github.com/robfig/cron/v3"
)

type slot struct {
	Start, End time.Time
	AllDay     bool
}

var (
	mu     sync.RWMutex
	perURL = map[string][]slot{}
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func midnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
}

func fetch(url string, from, to time.Time) ([]slot, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	c := gocal.NewParser(resp.Body)
	c.Start, c.End = &from, &to
	if err := c.Parse(); err != nil {
		return nil, err
	}
	var out []slot
	for _, e := range c.Events {
		if e.Start == nil {
			continue
		}
		s := *e.Start
		en := s
		if e.End != nil {
			en = *e.End
		}
		allDay := s.Hour() == 0 && s.Minute() == 0 && en.Sub(s) > 0 && en.Sub(s)%(24*time.Hour) == 0
		if allDay {
			s, en = midnight(s), midnight(en)
		} else {
			s, en = s.In(time.Local), en.In(time.Local)
		}
		out = append(out, slot{s, en, allDay})
	}
	return out, nil
}

func refresh(urls []string) {
	now := time.Now()
	from, to := now.AddDate(0, -6, 0), now.AddDate(1, 0, 0)
	for _, u := range urls {
		slots, err := fetch(u, from, to)
		if err != nil {
			log.Printf("fetch failed (keeping previous data): %v", err)
			continue
		}
		mu.Lock()
		perURL[u] = slots
		mu.Unlock()
	}
	log.Println("calendars refreshed")
}

type item struct {
	Start time.Time
	Text  string
}

// days returns busy items keyed by "2006-01-02".
func days() map[string][]item {
	res := map[string][]item{}
	mu.RLock()
	defer mu.RUnlock()
	for _, slots := range perURL {
		for _, s := range slots {
			last := s.End
			if s.End.After(s.Start) {
				last = s.End.Add(-time.Nanosecond)
			}
			for d := midnight(s.Start); !d.After(last); d = d.AddDate(0, 0, 1) {
				txt := "Busy"
				if !s.AllDay {
					a, b := s.Start, s.End
					if a.Before(d) {
						a = d
					}
					if next := d.AddDate(0, 0, 1); b.After(next) {
						b = next
					}
					txt = a.Format("15:04") + "–" + b.Format("15:04") + " Busy"
				}
				k := d.Format("2006-01-02")
				res[k] = append(res[k], item{s.Start, txt})
			}
		}
	}
	for k := range res {
		sort.Slice(res[k], func(i, j int) bool { return res[k][i].Start.Before(res[k][j].Start) })
	}
	return res
}

type cell struct {
	Day     int
	InMonth bool
	Today   bool
	Items   []string
}

type agendaDay struct {
	Label string
	Today bool
	Items []string
}

var page = template.Must(template.New("p").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Calendar</title>
<link href="https://cdn.jsdelivr.net/npm/beercss@3.11.33/dist/cdn/beer.min.css" rel="stylesheet">
<script type="module" src="https://cdn.jsdelivr.net/npm/beercss@3.11.33/dist/cdn/beer.min.js"></script>
<style>
.grid7{display:grid;grid-template-columns:repeat(7,minmax(0,1fr));gap:1px;background:var(--outline-variant,#cac4d0);border-radius:16px;overflow:hidden;border:1px solid var(--outline-variant,#cac4d0)}
.dow{background:var(--surface-container,#f3edf7);text-align:center;font-size:.75rem;font-weight:600;text-transform:uppercase;padding:.6rem 0;color:var(--on-surface-variant,#49454f)}
.day{background:var(--surface,#fef7ff);min-height:3.6rem;padding:.3rem .15rem;display:flex;flex-direction:column;align-items:center;gap:.25rem}
.day.out{opacity:.4}
.num{display:inline-flex;align-items:center;justify-content:center;width:1.9rem;height:1.9rem;border-radius:50%;font-size:.9rem}
.day.today .num{background:var(--primary,#6750a4);color:var(--on-primary,#fff);font-weight:700}
.dot{width:.5rem;height:.5rem;border-radius:50%;background:var(--error,#b3261e)}
.tag{display:none}
.busy{display:inline-block;padding:.1rem .6rem;border-radius:999px;background:var(--error-container,#f9dedc);color:var(--on-error-container,#410e0b);font-size:.85rem}
.agenda article.today{border-color:var(--primary,#6750a4)}
@media(min-width:700px){
 .day{min-height:7.5rem;align-items:stretch;padding:.4rem;gap:.3rem}
 .num{align-self:flex-start}
 .dot,.agenda{display:none}
 .tag{display:block;background:var(--error-container,#f9dedc);color:var(--on-error-container,#410e0b);border-radius:8px;font-size:.75rem;padding:.1rem .5rem;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
}
</style></head><body>
<main class="responsive">
 <nav>
  <a class="button circle transparent" href="?m={{.Prev}}" aria-label="Previous month"><i>chevron_left</i></a>
  <h5 class="max center-align no-margin">{{.Title}}</h5>
  <a class="button small-round tonal" href="?"><i>today</i><span>Today</span></a>
  <a class="button circle transparent" href="?m={{.Next}}" aria-label="Next month"><i>chevron_right</i></a>
 </nav>
 <div class="space"></div>
 <div class="grid7">
  <div class="dow">Mon</div><div class="dow">Tue</div><div class="dow">Wed</div><div class="dow">Thu</div><div class="dow">Fri</div><div class="dow">Sat</div><div class="dow">Sun</div>
  {{range .Days}}<div class="day{{if not .InMonth}} out{{end}}{{if .Today}} today{{end}}"><span class="num">{{.Day}}</span>{{if .Items}}<i class="dot"></i>{{end}}{{range .Items}}<div class="tag">{{.}}</div>{{end}}</div>
  {{end}}
 </div>
 <section class="agenda">
  {{range .Agenda}}<article class="border round{{if .Today}} today{{end}}"><h6 class="small">{{.Label}}</h6>{{range .Items}}<span class="busy">{{.}}</span> {{end}}</article>
  {{else}}<p class="center-align medium-opacity">Nothing busy this month.</p>
  {{end}}
 </section>
</main></body></html>`))

func handler(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	cur := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	if m := r.URL.Query().Get("m"); m != "" {
		if t, err := time.ParseInLocation("2006-01", m, time.Local); err == nil {
			cur = t
		}
	}
	busy := days()
	offset := (int(cur.Weekday()) + 6) % 7 // Monday-first
	d := cur.AddDate(0, 0, -offset)
	today := now.Format("2006-01-02")
	end := cur.AddDate(0, 1, 0)
	var cells []cell
	var agenda []agendaDay
	for d.Before(end) || len(cells)%7 != 0 {
		k := d.Format("2006-01-02")
		c := cell{Day: d.Day(), InMonth: d.Month() == cur.Month(), Today: k == today}
		for _, it := range busy[k] {
			c.Items = append(c.Items, it.Text)
		}
		if c.InMonth && len(c.Items) > 0 {
			agenda = append(agenda, agendaDay{d.Format("Mon, 2 Jan"), c.Today, c.Items})
		}
		cells = append(cells, c)
		d = d.AddDate(0, 0, 1)
	}
	page.Execute(w, map[string]any{
		"Title":  cur.Format("January 2006"),
		"Prev":   cur.AddDate(0, -1, 0).Format("2006-01"),
		"Next":   cur.AddDate(0, 1, 0).Format("2006-01"),
		"Days":   cells,
		"Agenda": agenda,
	})
}

func main() {
	var urls []string
	for _, u := range strings.Split(os.Getenv("ICAL_URLS"), ",") {
		if u = strings.TrimSpace(u); u != "" {
			urls = append(urls, u)
		}
	}
	if len(urls) == 0 {
		log.Fatal("ICAL_URLS is required (comma-separated list of iCal URLs)")
	}

	refresh(urls)
	c := cron.New() // standard 5-field cron expression
	if _, err := c.AddFunc(env("REFRESH_CRON", "*/15 * * * *"), func() { refresh(urls) }); err != nil {
		log.Fatalf("invalid REFRESH_CRON: %v", err)
	}
	c.Start()

	addr := ":" + env("PORT", "8080")
	http.HandleFunc("/", handler)
	log.Println("listening on", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
