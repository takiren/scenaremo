package script

// ApplyDefaults は省略されたフィールドに既定値を埋める。
//
// 既定値の解決をここで済ませておくことで、この先（タイムライン計算や props.json の生成）は
// 「省略されているかもしれない」を考えなくてよくなる。
// 埋める値は docs/schema.json の default と一致していること。
func ApplyDefaults(s *Script) {
	if s == nil {
		return
	}

	if s.Meta.Aspect == "" {
		s.Meta.Aspect = DefaultAspect
	}
	if s.Meta.FPS == 0 {
		s.Meta.FPS = DefaultFPS
	}

	for alias, speaker := range s.Speakers {
		if speaker.Engine == "" {
			speaker.Engine = DefaultEngine
			s.Speakers[alias] = speaker
		}
	}

	if s.Defaults == nil {
		s.Defaults = &Defaults{}
	}
	if s.Defaults.Transition == "" {
		s.Defaults.Transition = DefaultTransition
	}
	if s.Defaults.GapMs == nil {
		gap := DefaultGapMs
		s.Defaults.GapMs = &gap
	}
	if s.Defaults.SceneGapMs == nil {
		sceneGap := DefaultSceneGapMs
		s.Defaults.SceneGapMs = &sceneGap
	}

	for i := range s.Scenes {
		scene := &s.Scenes[i]
		if scene.Transition == "" {
			scene.Transition = s.Defaults.Transition
		}
		if scene.Component == "" {
			scene.Component = DefaultComponent
		}
		for j := range scene.Lines {
			line := &scene.Lines[j]
			if line.Speaker == "" {
				line.Speaker = s.Defaults.Speaker
			}
		}
	}
}
