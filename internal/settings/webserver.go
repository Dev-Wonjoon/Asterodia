package settings

import "encoding/json"

// Missing or invalid preferences retain the default code requirement.
//
//wails:ignore
func (s *Service) GetWebRequireCode() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	required := true
	_ = json.Unmarshal(s.values["webRequireCode"], &required)
	return required
}

//wails:ignore
func (s *Service) SetWebRequireCode(required bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.values["webRequireCode"]
	s.values["webRequireCode"], _ = json.Marshal(required)
	if err := s.persist(); err != nil {
		if existed {
			s.values["webRequireCode"] = previous
		} else {
			delete(s.values, "webRequireCode")
		}
		return err
	}
	return nil
}
