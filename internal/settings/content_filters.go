package settings

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Dev-Wonjoon/Asterodia/internal/media"
)

func (s *Service) GetContentFilters() media.ContentFilters {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := media.ContentFilters{HiddenPlatforms: []string{}, HiddenTags: []string{}}
	if raw := s.values["contentFilters"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &result)
	}
	return result
}

func (s *Service) SetContentFilters(value media.ContentFilters) (media.ContentFilters, error) {
	normalize := func(values []string) ([]string, error) {
		if len(values) > 100 {
			return nil, fmt.Errorf("숨김 목록은 각각 최대 100개입니다")
		}
		unique := map[string]bool{}
		for _, item := range values {
			item = strings.TrimSpace(item)
			if item == "" || !utf8.ValidString(item) || utf8.RuneCountInString(item) > 200 || strings.ContainsRune(item, 0) {
				return nil, fmt.Errorf("숨길 사이트와 태그 이름을 확인하세요")
			}
			unique[item] = true
		}
		result := make([]string, 0, len(unique))
		for item := range unique {
			result = append(result, item)
		}
		sort.Strings(result)
		return result, nil
	}
	var err error
	value.HiddenPlatforms, err = normalize(value.HiddenPlatforms)
	if err != nil {
		return media.ContentFilters{}, err
	}
	value.HiddenTags, err = normalize(value.HiddenTags)
	if err != nil {
		return media.ContentFilters{}, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return media.ContentFilters{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.values["contentFilters"]
	s.values["contentFilters"] = raw
	if err = s.persist(); err != nil {
		if existed {
			s.values["contentFilters"] = previous
		} else {
			delete(s.values, "contentFilters")
		}
		return media.ContentFilters{}, err
	}
	return value, nil
}
