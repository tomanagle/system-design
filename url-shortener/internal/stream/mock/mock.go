package mock

import (
	"url-shortener/internal/stream"

	"github.com/stretchr/testify/mock"
)

type ProducerMock struct {
	mock.Mock
}

func (m *ProducerMock) Produce(message stream.Message) bool {
	return m.Called(message).Bool(0)
}

type EventFactoryMock struct {
	mock.Mock
}

func (m *EventFactoryMock) NewEvent(code, userAgent, language string) stream.Event {
	return m.Called(code, userAgent, language).Get(0).(stream.Event)
}
