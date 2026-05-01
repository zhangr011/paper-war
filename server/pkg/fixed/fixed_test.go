package fixed

import (
	"math"
	"testing"
)

func TestFractionBits(t *testing.T) {
	if FractionBits != 12 {
		t.Errorf("FractionBits = %d, want 12", FractionBits)
	}
	if One != 1<<12 {
		t.Errorf("One = %d, want %d", One, 1<<12)
	}
}

func TestFromFloat(t *testing.T) {
	tests := []struct {
		f    float64
		want int64
	}{
		{0.0, 0},
		{1.0, 4096},
		{2.0, 8192},
		{-1.0, -4096},
		{0.5, 2048},
		{100.5, 411648},
	}
	for _, tt := range tests {
		got := FromFloat(tt.f)
		if got != tt.want {
			t.Errorf("FromFloat(%v) = %d, want %d", tt.f, got, tt.want)
		}
	}
}

func TestToFloat(t *testing.T) {
	tests := []struct {
		fix  int64
		want float64
	}{
		{0, 0.0},
		{4096, 1.0},
		{8192, 2.0},
		{-4096, -1.0},
		{2048, 0.5},
	}
	for _, tt := range tests {
		got := ToFloat(tt.fix)
		if math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("ToFloat(%d) = %v, want %v", tt.fix, got, tt.want)
		}
	}
}

func TestMul(t *testing.T) {
	a, b := FromFloat(2.0), FromFloat(3.0)
	got := Mul(a, b)
	want := FromFloat(6.0)
	if got != want {
		t.Errorf("Mul(%d, %d) = %d, want %d", a, b, got, want)
	}
	got = Mul(FromFloat(-2.0), FromFloat(3.0))
	want = FromFloat(-6.0)
	if got != want {
		t.Errorf("Mul(-2, 3) = %d, want %d", got, want)
	}
}

func TestDiv(t *testing.T) {
	a, b := FromFloat(6.0), FromFloat(3.0)
	got := Div(a, b)
	want := FromFloat(2.0)
	if got != want {
		t.Errorf("Div(%d, %d) = %d, want %d", a, b, got, want)
	}
}

func TestISqrt(t *testing.T) {
	got := ISqrt(FromFloat(9.0))
	want := FromFloat(3.0)
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	if diff > 50 {
		t.Errorf("ISqrt(9.0) = %d (~%v), want %d (~%v)", got, ToFloat(got), want, ToFloat(want))
	}
}

func TestDistSq(t *testing.T) {
	got := DistSq(FromFloat(3.0), FromFloat(4.0))
	want := FromFloat(25.0)
	if got != want {
		t.Errorf("DistSq(3,4) = %d, want %d", got, want)
	}
}

func TestClamp(t *testing.T) {
	got := Clamp(FromFloat(15.0), FromFloat(-10.0), FromFloat(10.0))
	want := FromFloat(10.0)
	if got != want {
		t.Errorf("Clamp(15, -10, 10) = %d, want %d", got, want)
	}
	got = Clamp(FromFloat(-15.0), FromFloat(-10.0), FromFloat(10.0))
	want = FromFloat(-10.0)
	if got != want {
		t.Errorf("Clamp(-15, -10, 10) = %d, want %d", got, want)
	}
}

func TestLerp(t *testing.T) {
	got := Lerp(FromFloat(0.0), FromFloat(10.0), FromFloat(0.5))
	want := FromFloat(5.0)
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	if diff > 10 {
		t.Errorf("Lerp(0, 10, 0.5) = %d, want %d", got, want)
	}
}

func TestAngleLerp(t *testing.T) {
	got := AngleLerp(3500, 100, FromFloat(0.5))
	if got > 1800 && got < 3400 {
		t.Errorf("AngleLerp(3500, 100, 0.5) = %d, should take short path through 0", got)
	}
}