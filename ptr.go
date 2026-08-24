package sarvam

func Float(v float64) *float64 { return &v }
func Int(v int) *int           { return &v }
func Bool(v bool) *bool        { return &v }

func String(v string) *string {
	s := v
	return &s
}
