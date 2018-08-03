package pool

import (
	"time"
)

const (
	defaultMaxNum          = 20
	defaultDialTimeout     = time.Millisecond * 200
	defaultMaxIdleDuration = time.Second * 30
)

type Option func(*Options)
type Options struct {
	InitNum         int
	MaxNum          int
	MaxIdleDuration time.Duration
	logger          logger
}

func newOptions(opts ...Option) Options {
	var sopt Options
	for _, opt := range opts {
		opt(&sopt)
	}

	checkOptions(&sopt)
	return sopt
}

func checkOptions(options *Options) {
	if options.MaxNum == 0 {
		options.MaxNum = defaultMaxNum
	}

	if options.MaxIdleDuration == 0 {
		options.MaxIdleDuration = defaultMaxIdleDuration
	}

	if options.logger == nil {
		options.logger = noopLogger{}
	}
}

func MaxNum(i int) Option {
	return func(options *Options) {
		options.MaxNum = i
	}
}
