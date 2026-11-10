package layout

import "testing"

func TestNested(t *testing.T) {
	n, e := Tree([]Pane{{ID: "a", Rect: Rect{0, 0, 40, 40}, CWD: "/a"}, {ID: "b", Rect: Rect{40, 0, 60, 20}, CWD: "/b"}, {ID: "c", Rect: Rect{40, 20, 60, 20}, CWD: "/c"}})
	if e != nil {
		t.Fatal(e)
	}
	if n.Direction != "right" || n.Ratio != 0.4 || n.Second.Direction != "down" || n.Second.Ratio != 0.5 {
		t.Fatalf("%+v", n)
	}
	if e = n.Validate(false); e != nil {
		t.Fatal(e)
	}
}
func TestInvalid(t *testing.T) {
	for _, ps := range [][]Pane{nil, {{Rect: Rect{0, 0, 0, 1}}}, {{Rect: Rect{0, 0, 10, 10}}, {Rect: Rect{1, 1, 10, 10}}}} {
		if _, e := Tree(ps); e == nil {
			t.Fatal("accepted invalid layout")
		}
	}
}

func TestExactRatioSurvivesPixelRounding(t *testing.T) {
	n, e := ExactTree([]Pane{{Rect: Rect{0, 0, 40, 40}, CWD: "/a"}, {Rect: Rect{40, 0, 80, 40}, CWD: "/b"}}, []Split{{Rect: Rect{0, 0, 120, 40}, Direction: "right", Ratio: 0.333}})
	if e != nil {
		t.Fatal(e)
	}
	if n.Ratio != 0.333 {
		t.Fatal(n.Ratio)
	}
}
