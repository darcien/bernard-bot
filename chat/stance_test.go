package chat

import (
	"testing"

	"bernard/tools"
)

// acceptsDefaultSnip lists tools that deliberately take the ReadOnly-tiered
// default instead of declaring their own. Membership is a decision with a
// reason, not a fallback.
var acceptsDefaultSnip = map[string]string{
	"current_time": "two dozen bytes of output; it will never reach minSnipBytes",
}

// A tool declaring neither would take whichever default its ReadOnly picks,
// chosen by nobody. Reasonix's TestEveryBuiltinDeclaresSnipStance, same shape.
func TestEveryToolDeclaresSnipStance(t *testing.T) {
	for _, tool := range builtinTools() {
		name := tool.Name()
		_, hints := tool.(tools.SnipHinter)
		_, listed := acceptsDefaultSnip[name]
		switch {
		case hints && listed:
			t.Errorf("%s both implements tools.SnipHinter and is listed in acceptsDefaultSnip; remove it from the list", name)
		case !hints && !listed:
			t.Errorf("tool %q declares no snip stance: implement tools.SnipHinter for a geometry of its own, or add it to acceptsDefaultSnip with the reason the ReadOnly-tiered default is right", name)
		}
	}
}

// A rename leaving a stale entry behind quietly stops guarding the tool.
func TestAcceptsDefaultSnipNamesRegisteredTools(t *testing.T) {
	registered := make(map[string]bool)
	for _, tool := range builtinTools() {
		registered[tool.Name()] = true
	}
	for name := range acceptsDefaultSnip {
		if !registered[name] {
			t.Errorf("acceptsDefaultSnip names %q, which is not registered", name)
		}
	}
}
