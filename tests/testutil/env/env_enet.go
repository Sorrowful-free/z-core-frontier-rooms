//go:build enet && cgo

package env

// WithEnet поднимает продовый delivery/enet mux-listener (P18): по host'у на
// порт из списка. Без аргументов - один host на свободном UDP-порту; элемент 0
// означает auto (свободный порт). Опция живёт только под `enet && cgo`: без
// тега listener был бы молчаливым no-op (stub-run), а callers ENet-клиентов и
// так обязаны быть tagged.
func WithEnet(ports ...uint16) Option {
	return func(o *options) {
		o.enet = true
		o.enetPorts = ports
	}
}

// EnetPort - первый UDP-порт ENet listener'а окружения; валиден только с
// WithEnet (иначе 0). Для multi-host окружений используйте EnetPorts или
// назначенный порт из IssueTicketPort/CreateRoomPort.
func (e *Env) EnetPort() uint16 {
	if len(e.enetPorts) == 0 {
		return 0
	}
	return e.enetPorts[0]
}

// EnetPorts - все UDP-порты ENet host'ов окружения (по host'у на порт, P18).
func (e *Env) EnetPorts() []uint16 {
	return e.enetPorts
}
