package validate

var (
	validCategoriesSlice = []string{"AI", "Blockchain", "Utilities", "Social Network",
		"Data", "Entertainment", "Productivity", "Lifestyle", "Developer", "Multimedia"}
)

func checkCategories(categories []string) bool {
	if len(categories) == 0 {
		return false
	}

	validCategories := map[string]bool{
		"AI":             true,
		"Blockchain":     true,
		"Utilities":      true,
		"Social Network": true,
		"Data":           true,
		"Entertainment":  true,
		"Productivity":   true,
		"Lifestyle":      true,
		"Developer":      true,
		"Multimedia":     true,
	}

	for _, category := range categories {
		if !validCategories[category] {
			return false
		}
	}

	return true
}
