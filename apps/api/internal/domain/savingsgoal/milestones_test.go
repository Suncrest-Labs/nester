package savingsgoal

import "testing"

func TestDetectNewMilestones(t *testing.T) {
	cases := []struct {
		name       string
		progress   float64
		notified   []int
		want       []int
	}{
		{"below first milestone", 24, nil, nil},
		{"exactly 25", 25, nil, []int{25}},
		{"already notified 25", 30, []int{25}, nil},
		{"cross multiple at once", 80, nil, []int{25, 50, 75}},
		{"100 completion", 100, []int{25, 50, 75}, []int{100}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectNewMilestones(tc.progress, tc.notified)
			if len(got) != len(tc.want) {
				t.Fatalf("DetectNewMilestones() = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("DetectNewMilestones() = %v, want %v", got, tc.want)
				}
			}
		})
	}
}
