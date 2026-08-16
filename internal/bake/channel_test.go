package bake

import (
	"reflect"
	"testing"
)

func TestChannelStaysConstant(t *testing.T) {
	c := channel[float32]{stride: 1}
	for i := 0; i < 10; i++ {
		c.pushOne(0.5)
	}
	if c.animated {
		t.Error("channel marked animated after ten identical pushes")
	}
	if want := []float32{0.5}; !reflect.DeepEqual(c.data, want) {
		t.Errorf("data = %v, want %v", c.data, want)
	}
	if c.frames != 10 {
		t.Errorf("frames = %d, want 10", c.frames)
	}
}

func TestChannelExpandsOnFirstChange(t *testing.T) {
	c := channel[int32]{stride: 1}
	c.pushOne(1)
	c.pushOne(1)
	c.pushOne(1)
	c.pushOne(2) // the value moves on frame 3
	c.pushOne(3)
	if !c.animated {
		t.Fatal("channel not marked animated")
	}
	want := []int32{1, 1, 1, 2, 3}
	if !reflect.DeepEqual(c.data, want) {
		t.Errorf("data = %v, want %v", c.data, want)
	}
	if c.frames != len(want) {
		t.Errorf("frames = %d, want %d", c.frames, len(want))
	}
}

func TestChannelWithStride(t *testing.T) {
	c := channel[uint16]{stride: 4}
	c.push([]uint16{1, 2, 3, 4})
	c.push([]uint16{1, 2, 3, 4})
	c.push([]uint16{9, 9, 9, 9})
	want := []uint16{1, 2, 3, 4, 1, 2, 3, 4, 9, 9, 9, 9}
	if !reflect.DeepEqual(c.data, want) {
		t.Errorf("data = %v, want %v", c.data, want)
	}
}

func TestChannelChangeOnSecondFrame(t *testing.T) {
	c := channel[uint8]{stride: 1}
	c.pushOne(0)
	c.pushOne(1)
	if want := []uint8{0, 1}; !reflect.DeepEqual(c.data, want) {
		t.Errorf("data = %v, want %v", c.data, want)
	}
}

func TestIsIdentityColor(t *testing.T) {
	if !isIdentityColor([]float32{1, 1, 1, 1, 0, 0, 0, 1}) {
		t.Error("the identity colour was not recognised")
	}
	if isIdentityColor([]float32{1, 0.5, 1, 1, 0, 0, 0, 1}) {
		t.Error("a tinted colour was treated as identity")
	}
	if !isIdentityColor(nil) {
		t.Error("an absent colour channel should count as identity")
	}
}
