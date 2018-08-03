package pool

type logger interface {
	Printf(fmt string, args ...interface{})
}

type noopLogger struct {
}

func (noopLogger) Printf(fmt string, args ...interface{}) {
}
