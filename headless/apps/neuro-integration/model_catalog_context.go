package main

import "fmt"

const modelCatalogResultLimit = 6

// Model lists prioritize usable IDs and next decisions; complete source and
// installation metadata remain available in the manager and item detail view.
func (n *NDIntegration) catalogSummaryLine(item CatalogItem, state ExtensionStateFile, stateErr error) string {
	status := "not installed"
	if stateErr != nil {
		status = "install state unavailable"
	} else if installed, ok := state.Installed[item.ID]; ok {
		status = "installed, disabled"
		if installed.Enabled {
			status = "installed, enabled"
		}
	}
	management := "management needs approval"
	if n.checkExtensionManagement(item.ID) == nil {
		management = "management delegated"
	}
	item = catalogPresentationItem(item, false)
	return fmt.Sprintf("- %s (%s): %s [%s; %s]", contextField(item.ID, 64),
		contextField(item.Name, 64), contextField(item.Description, 120), status, management)
}
