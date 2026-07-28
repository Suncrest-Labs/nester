package nudge

import (
	"bytes"
	"text/template"
	"fmt"
)

type TemplateCopyGenerator struct{}

func (g TemplateCopyGenerator) Generate(nudgeType NudgeType, facts Facts) (string, string, error) {
	var titleTmpl, bodyTmpl string

	switch nudgeType {
	case NudgeTypeDeadlineReminder:
		titleTmpl = "Your deadline is approaching!"
		bodyTmpl = "You have {{.TargetAmount}} {{.Currency}} left to reach your goal: {{.GoalName}} by {{.Deadline.Format \"Jan 02\"}}."
	case NudgeTypeGoalProximity:
		titleTmpl = "You're almost there!"
		bodyTmpl = "Just {{.TargetAmount}} {{.Currency}} left on {{.GoalName}} — you can finish this."
	case NudgeTypeStreakProtection:
		titleTmpl = "Keep your streak alive!"
		bodyTmpl = "You're on a {{.StreakWeeks}}-week streak. Deposit today to keep it going!"
	case NudgeTypeStreakMilestone:
		titleTmpl = "Streak milestone!"
		bodyTmpl = "{{.StreakWeeks}} weeks of consistent saving — nice work!"
	case NudgeTypeMilestone:
		titleTmpl = "Milestone Reached!"
		bodyTmpl = "You just hit a milestone for {{.GoalName}}! Keep up the great work."
	case NudgeTypePaydayDeposit:
		titleTmpl = "Good time to save?"
		bodyTmpl = "If you got paid recently, put a little toward {{.GoalName}} while it's fresh."
	default:
		titleTmpl = "A quick update"
		bodyTmpl = "Log in to check your latest savings."
	}

	title, err := render(titleTmpl, facts)
	if err != nil {
		return "", "", err
	}
	body, err := render(bodyTmpl, facts)
	if err != nil {
		return "", "", err
	}
	return title, body, nil
}

func render(tmpl string, facts Facts) (string, error) {
	t, err := template.New("nudge").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, facts); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}
	return buf.String(), nil
}
