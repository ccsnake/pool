package pool

import (
	"time"
)

const (
	defaultMaxNum          = 20
	defaultMaxIdleDuration = time.Second * 30
)

type Option func(*Options)
type Options struct {
	InitNum         int
	MaxActive       int
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
	if options.MaxIdleDuration == 0 {
		options.MaxIdleDuration = defaultMaxIdleDuration
	}

	if options.logger == nil {
		options.logger = noopLogger{}
	}
}

func MaxNum(i int) Option {
	return func(options *Options) {
		options.MaxActive = i
	}
}

func InitNum(i int) Option {
	return func(options *Options) {
		options.InitNum = i
	}
}

func MaxIdleDuration(d time.Duration) Option {
	return func(options *Options) {
		options.MaxIdleDuration = d
	}
}

func Logger(l logger) Option {
	return func(options *Options) {
		options.logger = l
	}
}
