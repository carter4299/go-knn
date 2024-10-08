package knn

import (
	"runtime"
	"sync"

	"github.com/alivanz/go-simd/arm"
	"github.com/alivanz/go-simd/arm/neon"
)

func (s *Search[T]) Manhattan(i *int) T {
	query := s.Query.Values.([]T)
	data := s.Data.Values.([][]T)[*i]

	if s.SIMD {
		var sum T
		n := len(query)
		for j := 0; j < n-3; j += 4 {
			var a, b arm.Float32X4
			var sub, abs arm.Float32X4

			for k := 0; k < 4; k++ {
				a[k] = arm.Float32(query[j+k])
				b[k] = arm.Float32(data[j+k])
			}

			neon.VsubqF32(&sub, &a, &b)
			neon.VabsqF32(&abs, &sub)
			for l := 0; l < 4; l++ {
				sum += T(abs[l])
			}
		}

		for j := n - n%4; j < n; j++ {
			sum += Abs(query[j] - data[j])
		}

		return sum
	}

	if len(query) > 128 {
		return s.manhattanUnrolled(query, data)
	}
	var sum T
	for j := 0; j < len(query); j++ {
		sum += Abs(query[j] - data[j])
	}
	return sum
}

func (s *Search[T]) manhattanUnrolled(query, data []T) T {
	var sum T
	n := len(query)

	for j := 0; j < n-3; j += 4 {
		sum += Abs(query[j]-data[j]) +
			Abs(query[j+1]-data[j+1]) +
			Abs(query[j+2]-data[j+2]) +
			Abs(query[j+3]-data[j+3])
	}

	for j := n - n%4; j < n; j++ {
		sum += Abs(query[j] - data[j])
	}

	return sum
}

func (s *Search[T]) Einsum() []T {
	qCols := s.Query.Shape[0]
	dRows := s.Data.Shape[0]
	result := make([]T, dRows)

	if s.Multithread {
		if s.MaxWorkers == 0 {
			s.MaxWorkers = runtime.NumCPU()
		}

		var wg sync.WaitGroup
		sem := make(chan struct{}, s.MaxWorkers)
		var mu sync.Mutex

		worker := func(s *Search[T], i int, wg *sync.WaitGroup) {
			defer wg.Done()
			dot := T(0)
			for j := 0; j < qCols; j++ {
				dot += s.Query.Values.([]T)[j] * s.Data.Values.([][]T)[i][j]
			}

			mu.Lock()
			result[i] = dot
			mu.Unlock()

			<-sem
		}

		for i := 0; i < dRows; i++ {
			sem <- struct{}{}
			wg.Add(1)
			go worker(s, i, &wg)
		}

		wg.Wait()
		close(sem)

		return result
	}

	for i := 0; i < dRows; i++ {
		dot := T(0)
		for j := 0; j < qCols; j++ {
			dot += s.Query.Values.([]T)[j] * s.Data.Values.([][]T)[i][j]
		}
		result[i] = dot
	}

	return result
}

func (s *Search[T]) HalfNorm() []T {
	dRows := s.Data.Shape[0]
	dCols := s.Data.Shape[1]
	result := make([]T, dRows)

	if s.Multithread {
		if s.MaxWorkers == 0 {
			s.MaxWorkers = runtime.NumCPU()
		}

		var wg sync.WaitGroup
		sem := make(chan struct{}, s.MaxWorkers)
		var mu sync.Mutex

		worker := func(s *Search[T], i int, wg *sync.WaitGroup) {
			defer wg.Done()
			norm := T(0)
			for j := 0; j < dCols; j++ {
				norm += s.Data.Values.([][]T)[i][j] * s.Data.Values.([][]T)[i][j]
			}

			mu.Lock()
			result[i] = norm * T(0.5)
			mu.Unlock()

			<-sem
		}

		for i := 0; i < dRows; i++ {
			sem <- struct{}{}
			wg.Add(1)
			go worker(s, i, &wg)
		}

		wg.Wait()
		close(sem)

		return result
	}

	for i := 0; i < dRows; i++ {
		norm := T(0)
		for j := 0; j < dCols; j++ {
			norm += s.Data.Values.([][]T)[i][j] * s.Data.Values.([][]T)[i][j]
		}
		result[i] = norm * T(0.5)
	}

	return result
}

func (s *Search[T]) EstimateBinSize() int {
	binSizes := []struct {
		threshold uint64
		value     uint64
	}{
		{1 << 8, 1},
		{1 << 12, 2},
		{1 << 16, 4},
		{1 << 20, 8},
		{1 << 24, 16},
		{1 << 28, 32},
	}

	for _, binSize := range binSizes {
		if uint64(s.Data.Shape[0]) < binSize.threshold {
			return int(binSize.value)
		}
	}
	return 64
}

func Abs[T float32 | float64](a T) T {
	if a < 0 {
		return -a
	}
	return a
}
