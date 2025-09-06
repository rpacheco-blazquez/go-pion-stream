package relay

import (
	"fmt"
	"sync"
)

// Stream represents a data stream (e.g., video/audio).
type Stream struct {
	ID      int
	Data    []byte
	Running bool
	Mutex   sync.Mutex
}

// Start begins the stream.
func (s *Stream) Start() error {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	if s.Running {
		return fmt.Errorf("stream %d is already running", s.ID)
	}
	s.Running = true
	return nil
}

// Stop halts the stream.
func (s *Stream) Stop() error {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	if !s.Running {
		return fmt.Errorf("stream %d is not running", s.ID)
	}
	s.Running = false
	return nil
}

// StartStream starts the stream for a specific channel.
func (ch *Channel) StartStream(streamID int) error {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	if stream, exists := ch.streamExist(streamID); exists {
		return stream.Start()
	}
	return fmt.Errorf("stream with ID %d does not exist in the channel", streamID)
}

// StopStream stops the stream for a specific channel.
func (ch *Channel) StopStream(streamID int) error {
	ch.Mutex.Lock()
	defer ch.Mutex.Unlock()
	if stream, exists := ch.streamExist(streamID); exists {
		return stream.Stop()
	}
	return fmt.Errorf("stream with ID %d does not exist in the channel", streamID)
}

// StartStreamForChannel starts the stream for a specific channel using ConnectionManager.
func (cm *ConnectionManager) StartStreamForChannel(channelCode string, streamID int) error {
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		return channel.StartStream(streamID)
	}
	return fmt.Errorf("channel with code %s does not exist", channelCode)
}

// StopStreamForChannel stops the stream for a specific channel using ConnectionManager.
func (cm *ConnectionManager) StopStreamForChannel(channelCode string, streamID int) error {
	if channel, exists := cm.ValidateChannel(channelCode); exists {
		return channel.StopStream(streamID)
	}
	return fmt.Errorf("channel with code %s does not exist", channelCode)
}
