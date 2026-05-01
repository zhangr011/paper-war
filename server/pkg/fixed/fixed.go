package fixed

const (
	FractionBits = 12
	One          = 1 << FractionBits // 4096
	Half         = One >> 1          // 2048
)

func FromFloat(f float64) int64 {
	return int64(f * float64(One))
}

func ToFloat(fix int64) float64 {
	return float64(fix) / float64(One)
}

func Mul(a, b int64) int64 {
	return (a * b) >> FractionBits
}

func Div(a, b int64) int64 {
	if b == 0 {
		return 0
	}
	return (a << FractionBits) / b
}

func ISqrt(val int64) int64 {
	if val <= 0 {
		return 0
	}
	x := val
	guess := int64(1) << ((bitLen(x) + FractionBits) / 2)
	for i := 0; i < 10; i++ {
		if guess == 0 {
			break
		}
		guess = (guess + Div(val, guess)) >> 1
	}
	return guess
}

func bitLen(x int64) int {
	n := 0
	for x > 0 {
		x >>= 1
		n++
	}
	return n
}

func DistSq(dx, dy int64) int64 {
	return (dx*dx + dy*dy) >> FractionBits
}

func Clamp(val, min, max int64) int64 {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

func Lerp(a, b, t int64) int64 {
	return a + Mul(b-a, t)
}

func AngleLerp(from, to int16, t int64) int16 {
	diff := int32(to) - int32(from)
	if diff > 1800 {
		diff -= 3600
	} else if diff < -1800 {
		diff += 3600
	}
	result := int32(from) + int32(Mul(int64(diff), t))
	result %= 3600
	if result < 0 {
		result += 3600
	}
	return int16(result)
}