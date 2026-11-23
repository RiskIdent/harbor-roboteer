package config

import (
	"iter"
	"slices"
)

// Set is a utility type consisting of only distinct values.
// Order is not guaranteed.
//
// It is implemented using slices, which gives higher performance and
// less memory use for smaller sizes (<100) compared to a map[T]struct{}
type Set[T comparable] []T

func NewSet[T comparable](capacity int) Set[T] {
	return make(Set[T], 0, capacity)
}

func (s Set[T]) Contains(value T) bool {
	return slices.Contains(s, value)
}

func (s *Set[T]) Add(value T) bool {
	if s.Contains(value) {
		return false
	}
	*s = append(*s, value)
	return true
}

func (s *Set[T]) AddSeq(values iter.Seq[T]) {
	for value := range values {
		s.Add(value)
	}
}

func (s *Set[T]) Delete(value T) bool {
	index := slices.Index(*s, value)
	if index == -1 {
		return false
	}
	last := len(*s) - 1
	(*s)[index] = (*s)[last]
	var zero T
	(*s)[last] = zero
	*s = (*s)[:last]
	return true
}
