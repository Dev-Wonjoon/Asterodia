package media

// Hidden parents hide their subtree consistently in gallery, search and explorer.
const visibilityAncestors = `(WITH RECURSIVE visibility_ancestors(id) AS (SELECT m.id UNION SELECT p.parent_id FROM album_parent p JOIN visibility_ancestors a ON p.album_id=a.id) SELECT id FROM visibility_ancestors)`

// ContentFilters are library display preferences, not file access permissions.
type ContentFilters struct {
	HiddenPlatforms []string `json:"hiddenPlatforms"`
	HiddenTags      []string `json:"hiddenTags"`
}

//wails:ignore
func (s *Service) ConfigureContentFilters(read func() ContentFilters) { s.contentFilters = read }

func (s *Service) GetContentFilters() ContentFilters {
	if s.contentFilters == nil {
		return ContentFilters{HiddenPlatforms: []string{}, HiddenTags: []string{}}
	}
	return s.contentFilters()
}

func (s *Service) visibleFilter(filter Filter) Filter {
	if !filter.ShowHidden {
		preferences := s.GetContentFilters()
		filter.hiddenPlatforms = preferences.HiddenPlatforms
		filter.hiddenTags = preferences.HiddenTags
	}
	return filter
}
