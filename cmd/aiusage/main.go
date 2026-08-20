// aiusage watches two things for one person: their Claude.ai quota meters
// and their GitHub Copilot AI-credit consumption. It polls on a timer and
// pings Discord whenever a Claude meter climbs into a step it hasn't been
// announced at before.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	// The runtime image (distroless/static) ships no system tzdata, so
	// named zones like "Europe/Stockholm" would otherwise fail to load.
	_ "time/tzdata"

	"ai-usage-mini/internal/claudeusage"
	"ai-usage-mini/internal/copilotcredit"
	"ai-usage-mini/internal/notify"
	"ai-usage-mini/internal/settings"
	"ai-usage-mini/internal/thresholds"
)

func main() {
	singleShot := flag.Bool("once", false, "poll a single time and exit, instead of running forever")
	flag.Parse()

	cfg, err := settings.FromEnv()
	if err != nil {
		log.Fatalf("reading settings: %v", err)
	}

	ledger, err := thresholds.Open(cfg.LedgerPath)
	if err != nil {
		log.Printf("ledger unreadable, starting fresh: %v", err)
		ledger = &thresholds.Ledger{Announced: map[string]int{}}
	}

	displayLoc, err := time.LoadLocation(cfg.DisplayTZ)
	if err != nil {
		log.Fatalf("DISPLAY_TZ %q: %v", cfg.DisplayTZ, err)
	}

	checker := &Checker{
		cfg:        cfg,
		claude:     claudeusage.New(cfg.AnthropicOrg, cfg.AnthropicSession),
		copilot:    copilotcredit.New(cfg.CopilotOrg, cfg.CopilotToken),
		discord:    notify.NewDiscord(cfg.DiscordHook),
		ledger:     ledger,
		displayLoc: displayLoc,
	}

	if *singleShot {
		checker.Run()
		return
	}

	log.Printf("polling every %s until stopped", cfg.PollEvery)
	for {
		checker.Run()
		time.Sleep(cfg.PollEvery)
	}
}

// Checker owns one poll cycle: fetch both sources, compare Claude's meters
// against the on-disk ledger, and notify Discord for anything newly
// noteworthy.
type Checker struct {
	cfg        settings.Settings
	claude     *claudeusage.Client
	copilot    *copilotcredit.Client
	discord    *notify.Discord
	ledger     *thresholds.Ledger
	displayLoc *time.Location
}

func (c *Checker) Run() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	reading := c.pollClaude(ctx)
	report := c.pollCopilot(ctx)

	log.Print(oneLineStatus(reading, report))

	if reading == nil {
		return
	}

	var raised []claudeusage.Meter
	for _, m := range reading.Meters {
		if !m.Live {
			continue
		}
		step := stepFor(m.Kind)
		if c.ledger.Advance(m.ID(), floorToStep(m.PctUsed, step)) {
			raised = append(raised, m)
		}
	}

	if err := c.ledger.Persist(c.cfg.LedgerPath); err != nil {
		log.Printf("could not save ledger: %v", err)
	}

	if len(raised) == 0 {
		return
	}

	body := composeAlert(reading, raised, report, c.displayLoc)
	if c.cfg.DiscordHook == "" {
		log.Printf("would notify (no DISCORD_WEBHOOK_URL configured):\n%s", body)
		return
	}
	if err := c.discord.Send(body); err != nil {
		log.Printf("discord notification failed: %v", err)
	}
}

func (c *Checker) pollClaude(ctx context.Context) *claudeusage.Reading {
	if !c.cfg.HasClaude() {
		log.Print("claude: not configured, skipping")
		return nil
	}
	reading, err := c.claude.Poll(ctx)
	if err != nil {
		log.Printf("claude: %v", err)
		return nil
	}
	return reading
}

func (c *Checker) pollCopilot(ctx context.Context) *copilotcredit.Report {
	if !c.cfg.HasCopilot() {
		log.Print("copilot: not configured, skipping")
		return nil
	}
	report, err := c.copilot.ForLogin(ctx, c.cfg.WatchedLogin)
	if err != nil {
		log.Printf("copilot: %v", err)
		return nil
	}
	return report
}

// stepFor returns how many percentage points must pass before a meter's
// climb is worth a new alert. "session" resets roughly every five hours, so
// a fine step would fire almost continuously; it gets a coarse 25-point
// step. The weekly meter ("weekly_scoped") moves much more slowly, so a
// 10-point step is fine enough to still catch meaningful jumps.
func stepFor(kind string) int {
	if kind == "session" {
		return 25
	}
	return 10
}

func floorToStep(pct float64, step int) int {
	n := int(pct)
	n -= n % step
	switch {
	case n < 0:
		return 0
	case n > 100:
		return 100
	default:
		return n
	}
}

func oneLineStatus(reading *claudeusage.Reading, report *copilotcredit.Report) string {
	var parts []string
	if reading != nil {
		for _, m := range reading.Meters {
			if m.Live {
				parts = append(parts, fmt.Sprintf("claude.%s=%.0f%%", m.ID(), m.PctUsed))
			}
		}
	}
	if report != nil {
		parts = append(parts, fmt.Sprintf("copilot.%s: in-plan=%.0f overage=%.0f",
			report.Login, report.InPlanTotal(), report.OverageTotal()))
	}
	if len(parts) == 0 {
		return "poll: nothing configured"
	}
	return "poll: " + strings.Join(parts, " ")
}

// composeAlert renders every Claude meter (marking the ones in raised) plus
// a snapshot of Copilot credit usage as context, so a message always shows
// the full picture rather than just whichever meter happened to trigger it.
// Copilot has no step of its own here — one person's draw against an
// org-wide credit pool isn't a meaningful percentage by itself — so it just
// rides along with whatever triggered the message.
func composeAlert(reading *claudeusage.Reading, raised []claudeusage.Meter, report *copilotcredit.Report, displayLoc *time.Location) string {
	raisedIDs := map[string]bool{}
	for _, m := range raised {
		raisedIDs[m.ID()] = true
	}

	var b strings.Builder
	b.WriteString("**Claude quota update**\n")
	for _, m := range reading.Meters {
		flag := "•"
		if raisedIDs[m.ID()] {
			flag = "▲"
		}
		fmt.Fprintf(&b, "%s %s: %.0f%%", flag, m.ID(), m.PctUsed)
		if m.ResetsAt != nil {
			fmt.Fprintf(&b, " (resets %s)", m.ResetsAt.In(displayLoc).Format("Mon 15:04"))
		}
		b.WriteString("\n")
	}

	if report != nil {
		fmt.Fprintf(&b, "\nCopilot credits, %04d-%02d, %s: in-plan=%.0f overage=%.0f\n",
			report.Year, report.Month, report.Login, report.InPlanTotal(), report.OverageTotal())
	}

	return b.String()
}
