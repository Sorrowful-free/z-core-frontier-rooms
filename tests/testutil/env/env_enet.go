//go:build enet && cgo

package env

// WithEnet поднимает продовый delivery/enet listener на свободном UDP-порту
// (см. wiring в env.New). Опция живёт только под `enet && cgo`: без тега
// listener был бы молчаливым no-op (stub-run), а callers ENet-клиентов и так
// обязаны быть tagged.
func WithEnet() Option {
	return func(o *options) {
		o.enet = true
	}
}

// EnetPort — UDP-порт ENet listener'а окружения; валиден только с WithEnet
// (иначе 0).
func (e *Env) EnetPort() uint16 {
	return e.enetPort
}
