package api

import (
	"sort"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// Rendering a rule and its tuning for the screen that administers it.

// levelNames maps a rung of the tenancy ladder to how the API spells it. The
// ladder itself lives in internal/settings; this is only its spelling on the
// wire, and it exists in one place so a rung cannot be spelled two ways.
var levelNames = map[settings.Level]RuleBindingLevel{
	settings.LevelDevice:       RuleBindingLevelDevice,
	settings.LevelSite:         RuleBindingLevelSite,
	settings.LevelOrganization: RuleBindingLevelOrganization,
	settings.LevelTenant:       RuleBindingLevelTenant,
}

// levelByName is levelNames read the other way, for values coming in.
var levelByName = func() map[RuleBindingLevel]settings.Level {
	out := make(map[RuleBindingLevel]settings.Level, len(levelNames))
	for level, name := range levelNames {
		out[name] = level
	}
	return out
}()

// resolvedLevelNames is the same ladder plus the floor a value falls to when
// nothing overrode it.
var resolvedLevelNames = map[settings.Level]ResolvedRuleParameterLevel{
	settings.LevelDevice:       ResolvedRuleParameterLevelDevice,
	settings.LevelSite:         ResolvedRuleParameterLevelSite,
	settings.LevelOrganization: ResolvedRuleParameterLevelOrganization,
	settings.LevelTenant:       ResolvedRuleParameterLevelTenant,
	settings.LevelShipped:      ResolvedRuleParameterLevelShipped,
}

// noiseLevels maps how noisy a rule has been to the colour the badge takes.
var noiseLevels = map[alerts.NoiseLevel]RuleNoiseLevel{
	alerts.NoiseUnknown:  Unknown,
	alerts.NoiseQuiet:    Quiet,
	alerts.NoiseUsual:    Usual,
	alerts.NoiseElevated: Elevated,
	alerts.NoiseHigh:     High,
}

// stageNames maps how far a rollout has reached to its name on the wire.
var stageNames = map[rules.Stage]RuleStage{
	rules.StageOff:    Off,
	rules.StageCanary: Canary,
	rules.StageStaged: Staged,
	rules.StageFull:   Full,
}

// noiseToAPI renders the badge. A rule nothing has raised for is quiet with no
// history, which reads neutral rather than alarming — a fresh customer whose
// whole pack showed red would be told nothing at all.
func noiseToAPI(n alerts.Noise) RuleNoise {
	return RuleNoise{
		Recent:          n.Recent,
		BaselinePerHour: n.BaselinePerHour,
		Level:           noiseLevels[n.Level()],
	}
}

// rolloutToAPI renders how far a rule has reached and the pace it spreads at.
func rolloutToAPI(r rules.Rollout) RuleRollout {
	out := RuleRollout{
		Enabled:        r.Enabled,
		RolloutPercent: r.RolloutPercent,
		Kill:           r.Kill,
		Stage:          stageNames[r.Stage()],
		CanaryPercent:  r.PercentForStage(rules.StageCanary),
		StagedPercent:  r.PercentForStage(rules.StageStaged),
		CanaryHoldSecs: int(r.HoldFor(rules.StageCanary).Seconds()),
		StagedHoldSecs: int(r.HoldFor(rules.StageStaged).Seconds()),
	}
	if r.CanaryGroup != "" {
		group := r.CanaryGroup
		out.CanaryGroup = &group
	}
	return out
}

// bindingToAPI renders one tuned value.
func bindingToAPI(b rules.Binding) RuleBinding {
	return RuleBinding{
		Id:         b.ID,
		Level:      levelNames[b.Level],
		LevelKey:   b.LevelKey,
		Selector:   selectorToAPI(b.Selector),
		Precedence: b.Precedence,
		Params:     paramsToAPI(b.Params),
		UpdatedBy:  b.UpdatedBy,
	}
}

// bindingsToAPI renders a whole rule's tuning, ordered narrowest rung first so
// the screen reads the way resolution does.
func bindingsToAPI(bindings []rules.Binding) []RuleBinding {
	out := make([]RuleBinding, 0, len(bindings))
	for _, b := range bindings {
		out = append(out, bindingToAPI(b))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Level != out[j].Level {
			return levelByName[out[i].Level] < levelByName[out[j].Level]
		}
		return out[i].Precedence > out[j].Precedence
	})
	return out
}

// bindingFromAPI reads a tuned value an administrator stated. A value with no id
// is a new one, so an id is minted here rather than by the client — a client
// choosing row identities is a client that can overwrite somebody else's.
func bindingFromAPI(ruleID string, organizationID uuid.UUID, in RuleBindingInput) rules.Binding {
	b := rules.Binding{
		ID:             uuid.New(),
		OrganizationID: organizationID,
		RuleID:         ruleID,
		Level:          levelByName[in.Level],
		LevelKey:       in.LevelKey,
		Params:         paramsFromAPI(in.Params),
	}
	if in.Id != nil {
		b.ID = *in.Id
	}
	if in.Precedence != nil {
		b.Precedence = *in.Precedence
	}
	if in.Selector != nil {
		b.Selector = rules.Selector(*in.Selector)
	}
	return b
}

// selectorToAPI renders the labels a tuned value is aimed at, as an object
// rather than null so a reader never has to tell one from the other.
func selectorToAPI(s rules.Selector) map[string]string {
	out := make(map[string]string, len(s))
	for key, value := range s {
		out[key] = value
	}
	return out
}

// paramsToAPI renders the numbers a tuned value carries.
func paramsToAPI(params map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(params))
	for name, value := range params {
		out[name] = value
	}
	return out
}

// paramsFromAPI reads them back.
func paramsFromAPI(params map[string]float64) map[string]float64 {
	if params == nil {
		return map[string]float64{}
	}
	return paramsToAPI(params)
}

// clampsToAPI renders what a rule version had to move.
func clampsToAPI(clamps []rules.Clamp) []RuleClamp {
	out := make([]RuleClamp, 0, len(clamps))
	for _, c := range clamps {
		out = append(out, RuleClamp{
			Id:          c.ID,
			BindingId:   c.BindingID,
			RuleId:      c.RuleID,
			RuleVersion: c.RuleVersion,
			Param:       c.Param,
			FromValue:   c.From,
			ToValue:     c.To,
			ClampedAt:   c.ClampedAt.UTC(),
		})
	}
	return out
}

// resolvedParamsToAPI renders every parameter as it applies to one machine, and
// what decided it. The value comes from the same resolution the delivery path
// runs, so the screen cannot disagree with the wire; the attribution is worked
// out beside it from the same bindings.
func resolvedParamsToAPI(
	definition rules.Definition, machine rules.Device, bindings []rules.Binding,
) map[string]ResolvedRuleParameter {
	resolved := rules.Resolve(definition, machine, bindings)
	wire := map[string]float64{
		"threshold":    resolved.Threshold,
		"clear":        resolved.Clear,
		"sustain_secs": float64(resolved.SustainSecs),
		"window_secs":  float64(resolved.WindowSecs),
	}

	out := make(map[string]ResolvedRuleParameter, len(wire))
	for name, value := range wire {
		level, source := rules.DecidedBy(definition, machine, bindings, name)
		out[name] = ResolvedRuleParameter{
			Value:  value,
			Level:  resolvedLevelNames[level],
			Source: source,
		}
	}
	return out
}

// limitsToAPI renders a customer's budget beside the maxima the code allows, so
// the screen can say how far a number may move without asking again.
func limitsToAPI(l alerts.Limits) AlertLimits {
	return AlertLimits{
		OrganizationHourly:    l.OrganizationHourly,
		DeviceHourly:          l.DeviceHourly,
		MaxOrganizationHourly: alerts.MaxOrganizationHourlyCeiling,
		MaxDeviceHourly:       alerts.MaxDeviceHourlyCeiling,
		UpdatedBy:             l.UpdatedBy,
	}
}

// labelToAPI renders one entry in a customer's label list.
func labelToAPI(l rules.Label) DeviceTagLabel {
	return DeviceTagLabel{Id: l.ID, Key: l.Key, Value: l.Value, CreatedBy: l.CreatedBy}
}

// labelsToAPI renders the list.
func labelsToAPI(labels []rules.Label) []DeviceTagLabel {
	out := make([]DeviceTagLabel, 0, len(labels))
	for _, l := range labels {
		out = append(out, labelToAPI(l))
	}
	return out
}

// assignmentsToAPI renders who carries what, ordered by machine so the list is
// stable across reads.
func assignmentsToAPI(assignments map[uuid.UUID]map[string]string) []DeviceTagAssignment {
	out := make([]DeviceTagAssignment, 0, len(assignments))
	for deviceID, tags := range assignments {
		out = append(out, DeviceTagAssignment{DeviceId: deviceID, Tags: tags})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].DeviceId.String() < out[j].DeviceId.String()
	})
	return out
}
