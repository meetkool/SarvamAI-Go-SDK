package sarvam

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"iter"
)

type Stream[T any] struct {
	body    io.ReadCloser
	reader  *bufio.Reader
	current T
	err     error
	closed  bool
	onFirst func()
	first   bool
}

func newStream[T any](body io.ReadCloser) *Stream[T] {
	return &Stream[T]{body: body, reader: bufio.NewReader(body)}
}

func (s *Stream[T]) Next() bool {
	if s.closed {
		return false
	}
	data, err := s.readEvent()
	if err != nil {
		if err != io.EOF {
			s.err = err
		}
		s.Close()
		return false
	}

	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		s.err = fmt.Errorf("%w: %w", ErrDecode, err)
		s.Close()
		return false
	}
	s.current = v
	if !s.first {
		s.first = true
		if s.onFirst != nil {
			s.onFirst()
		}
	}
	return true
}

func (s *Stream[T]) Chunk() T   { return s.current }
func (s *Stream[T]) Err() error { return s.err }

func (s *Stream[T]) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.body.Close()
}

func (s *Stream[T]) All() iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for s.Next() {
			if !yield(s.Chunk(), nil) {
				return
			}
		}
		if err := s.Err(); err != nil {
			var zero T
			yield(zero, err)
		}
	}
}

func (s *Stream[T]) readEvent() ([]byte, error) {
	var data [][]byte
	for {
		line, err := s.reader.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return nil, err
		}
		atEOF := err == io.EOF
		line = bytes.TrimRight(line, "\r\n")

		if value, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			data = append(data, bytes.TrimPrefix(value, []byte(" ")))
		}

		if (len(line) == 0 || atEOF) && len(data) > 0 {
			payload := bytes.Join(data, []byte("\n"))
			if bytes.Equal(bytes.TrimSpace(payload), []byte("[DONE]")) {
				return nil, io.EOF
			}
			return payload, nil
		}
		if atEOF {
			return nil, io.EOF
		}
	}
}
