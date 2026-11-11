package layout

// OperatorSection is one section of the operator area (docs/ui/design.md,
// Operator area). Every operator page names the section it belongs to, so the
// rail marks it.
type OperatorSection int

// The sections of the operator area, in rail order.
const (
	OperatorOverview OperatorSection = iota
	OperatorInvitations
	OperatorSuggestions
	OperatorIngredients
	OperatorEquipment
	OperatorDeployment
	OperatorUsage
)

// operatorItem is one entry of the operator rail.
type operatorItem struct {
	Section OperatorSection
	Label   string
	Path    string
	Icon    IconName
}

// operatorSections lists the rail in display order. Paths are written out, not
// imported from the feature packages, for the same reason navItems' are
// (ADR-00012); package web checks that each one is served.
var operatorSections = []operatorItem{
	{OperatorOverview, "Overview", OperatorPath, IconInfo},
	{OperatorInvitations, "Invitations", OperatorPath + "/invitations", IconMail},
	{OperatorSuggestions, "Suggested ingredients", OperatorPath + "/promotions", IconBulb},
	{OperatorIngredients, "Central ingredients", OperatorPath + "/ingredients", IconTag},
	{OperatorEquipment, "Equipment", OperatorPath + "/equipment", IconPot},
	{OperatorDeployment, "Deployment", OperatorPath + "/settings", IconGear},
	{OperatorUsage, "LLM usage", OperatorPath + "/usage", IconSparkles},
}

// OperatorSectionPaths returns the path of every rail entry, in rail order, so
// package web can check that each is served.
func OperatorSectionPaths() []string {
	paths := make([]string, len(operatorSections))
	for i, it := range operatorSections {
		paths[i] = it.Path
	}
	return paths
}

// operatorLabel returns the rail label of section s, which the breadcrumb
// shows.
func operatorLabel(s OperatorSection) string {
	for _, it := range operatorSections {
		if it.Section == s {
			return it.Label
		}
	}
	return ""
}

// operatorPath returns the path of section s.
func operatorPath(s OperatorSection) string {
	for _, it := range operatorSections {
		if it.Section == s {
			return it.Path
		}
	}
	return OperatorPath
}
