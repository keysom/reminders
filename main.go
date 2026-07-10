package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const dateLayout = "2006-01-02"

type Config struct {
	Timezone    string     `yaml:"timezone"`
	RemindDays  []int      `yaml:"remind_days"`
	AlwaysPush  bool       `yaml:"always_push"`
	PushPlusURL string     `yaml:"pushplus_url"`
	Reminders   []Reminder `yaml:"reminders"`
}

type Reminder struct {
	Name       string `yaml:"name"`
	Date       string `yaml:"date"`
	Repeat     string `yaml:"repeat"`
	RemindDays []int  `yaml:"remind_days"`
	Note       string `yaml:"note"`
	Enabled    *bool  `yaml:"enabled"`
}

type Hit struct {
	Reminder Reminder
	EventDay time.Time
	DaysLeft int
}

func main() {
	configPath := flag.String("config", "reminders.yml", "path to reminders config")
	todayArg := flag.String("today", "", "override today date, format YYYY-MM-DD")
	dryRun := flag.Bool("dry-run", false, "print result without pushing")
	flag.Parse()

	if err := run(*configPath, *todayArg, *dryRun); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(configPath, todayArg string, dryRun bool) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	loc, err := time.LoadLocation(defaultString(cfg.Timezone, "Asia/Shanghai"))
	if err != nil {
		return fmt.Errorf("load timezone: %w", err)
	}

	now := time.Now().In(loc)
	if todayArg != "" {
		now, err = time.ParseInLocation(dateLayout, todayArg, loc)
		if err != nil {
			return fmt.Errorf("parse --today: %w", err)
		}
	}

	today := startOfDay(now)
	hits, err := collectHits(cfg, today)
	if err != nil {
		return err
	}

	message := renderMessage(today, hits)
	fmt.Println(message)

	if dryRun || envBool("DRY_RUN") {
		return nil
	}

	if len(hits) == 0 && !cfg.AlwaysPush && !envBool("ALWAYS_PUSH") {
		fmt.Println("no matched reminders, skip push")
		return nil
	}

	token := strings.TrimSpace(os.Getenv("PUSHPLUS_TOKEN"))
	if token == "" {
		return errors.New("PUSHPLUS_TOKEN is empty; set it in GitHub Secrets or run with --dry-run")
	}

	pushURL := defaultString(cfg.PushPlusURL, "https://www.pushplus.plus/send")
	return sendPushPlus(pushURL, token, "日期提醒", message)
}

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	if len(cfg.RemindDays) == 0 {
		cfg.RemindDays = []int{7, 3, 1, 0}
	}

	return cfg, nil
}

func collectHits(cfg Config, today time.Time) ([]Hit, error) {
	hits := make([]Hit, 0)

	for _, item := range cfg.Reminders {
		if item.Enabled != nil && !*item.Enabled {
			continue
		}
		if strings.TrimSpace(item.Name) == "" {
			return nil, errors.New("reminder name cannot be empty")
		}

		days := item.RemindDays
		if len(days) == 0 {
			days = cfg.RemindDays
		}

		eventDay, daysLeft, err := nextEventDay(item, today)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", item.Name, err)
		}

		if containsInt(days, daysLeft) {
			hits = append(hits, Hit{
				Reminder: item,
				EventDay: eventDay,
				DaysLeft: daysLeft,
			})
		}
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].DaysLeft == hits[j].DaysLeft {
			return hits[i].Reminder.Name < hits[j].Reminder.Name
		}
		return hits[i].DaysLeft < hits[j].DaysLeft
	})

	return hits, nil
}

func nextEventDay(item Reminder, today time.Time) (time.Time, int, error) {
	base, err := time.ParseInLocation(dateLayout, item.Date, today.Location())
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("parse date %q: %w", item.Date, err)
	}

	repeat := strings.ToLower(strings.TrimSpace(item.Repeat))
	if repeat == "" {
		repeat = "once"
	}

	var eventDay time.Time
	switch repeat {
	case "once", "none":
		eventDay = startOfDay(base)
	case "yearly", "annual":
		eventDay = safeDate(today.Location(), today.Year(), int(base.Month()), base.Day())
		if eventDay.Before(today) {
			eventDay = safeDate(today.Location(), today.Year()+1, int(base.Month()), base.Day())
		}
	case "monthly":
		eventDay = safeDate(today.Location(), today.Year(), int(today.Month()), base.Day())
		if eventDay.Before(today) {
			nextMonth := today.AddDate(0, 1, 0)
			eventDay = safeDate(today.Location(), nextMonth.Year(), int(nextMonth.Month()), base.Day())
		}
	default:
		return time.Time{}, 0, fmt.Errorf("unsupported repeat %q", item.Repeat)
	}

	return eventDay, int(eventDay.Sub(today).Hours() / 24), nil
}

func safeDate(loc *time.Location, year, month, day int) time.Time {
	firstOfNextMonth := time.Date(year, time.Month(month)+1, 1, 0, 0, 0, 0, loc)
	lastDay := firstOfNextMonth.AddDate(0, 0, -1).Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, loc)
}

func renderMessage(today time.Time, hits []Hit) string {
	var b strings.Builder

	fmt.Fprintf(&b, "今天是 %s\n\n", today.Format(dateLayout))

	if len(hits) == 0 {
		b.WriteString("今天没有需要提醒的事项。")
		return b.String()
	}

	b.WriteString("需要提醒的事项：\n")
	for _, hit := range hits {
		when := "今天"
		if hit.DaysLeft > 0 {
			when = fmt.Sprintf("%d 天后", hit.DaysLeft)
		}

		fmt.Fprintf(&b, "- %s：%s（%s）", hit.Reminder.Name, when, hit.EventDay.Format(dateLayout))
		if strings.TrimSpace(hit.Reminder.Note) != "" {
			fmt.Fprintf(&b, "\n  %s", hit.Reminder.Note)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func sendPushPlus(pushURL, token, title, content string) error {
	payload := map[string]string{
		"token":    token,
		"title":    title,
		"content":  content,
		"template": "txt",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, pushURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("pushplus request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("pushplus status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	fmt.Println("pushplus response:", strings.TrimSpace(string(respBody)))
	return nil
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func containsInt(nums []int, target int) bool {
	for _, n := range nums {
		if n == target {
			return true
		}
	}
	return false
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func envBool(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
