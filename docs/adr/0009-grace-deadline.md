# Reconnect: enforce grace-дедлайна на resume (P24)

ADR-0008 ввёл grace-модель: разрыв → `MarkDisconnected` (lastSeen на admitted-слоте) → resume в пределах `RECONNECT_TTL`, expiry — фоновым `SweepOrphanAdmittedUseCase`. Пробел: дедлайн проверялся **только** sweep'ом. Resume после `lastSeen + RECONNECT_TTL`, но до прохода sweep, проходил успешно; при `RESERVATION_ORPHAN_SWEEP_INTERVAL=0` grace был бессмертен — reconnect-токен вечный, пока жив слот.

Механический нюанс: `reservation.Reconnect` сразу перезаписывает `admittedAt = now` — UC не видит старый lastSeen, а разнесённая проверка без защиты от refresh'а дырява: отклонённая попытка сама продлевает grace, и повторная уже проходит.

## Decision

- **Дедлайн enforced на resume-потоке**, не только в sweep. `ReconnectRoomUseCase` получает `reconnectTTL` в конструктор (`cfg.Reservation.ReconnectTTL`) и проверяет `!room.HasPeer(peerID) && now.After(slot.AdmittedAt + reconnectTTL)` → `ErrReservationExpired` → reject `0x74` (существующий маппинг `delivery/errors`).
- **Проверка до refresh'а — через read-only `SlotDetails`.** Новый метод порта `reservation.SlotDetails(roomID, peerID)` возвращает admitted-слот **без** обновления `admittedAt` (неизменяющий вариант пути `touchAdmitted`: `lookupRoom` + `room.mu` + `checkLive` + admitted-проверка). UC читает честный lastSeen, гейтит, и только потом вызывает `Reconnect` (refresh). Rejected попытка не продлевает grace — повторные тоже видят expired, sweep revoke освобождает слот.
- **Живой peer гейтом не затрагивается.** `admittedAt` давно подключённого peer'а старый законно — дедлайн на него не распространяется; `HasPeer → Replace` (newest-wins) идёт без гейта. `reconnectTTL <= 0` запрещает именно воскрешение (`!HasPeer` resume всегда expired — ноль-семантизм sweep), не перехват живого соединения.

## Considered Options

- **Флаг-параметр `enforceDeadline` в `Reconnect`** — отклонено: flag-аргумент, check/refresh смешаны в одном методе, rejected попытка всё равно освежала бы lastSeen до отклонения (гейт после входа в мутирующий путь).
- **Дедлайн только в sweep, увеличив частоту** — отклонено: sweep — best-effort фоновый механизм, `interval=0` его отключает; grace-дедлайн — семантика admit-потока и должен держаться без фонового прогона.

## Consequences

- Resume после `lastSeen + RECONNECT_TTL` отклоняется `0x74` немедленно, не дожидаясь sweep — reconnect-токен конечен даже при выключенном sweep.
- Клиент получает различимые reject'ы: слот умер (`0x73` NotReserved — например, sweep выиграл гонку до `Reconnect`) vs grace истёк (`0x74` Expired).
- Порядок `SlotDetails → гейт → Reconnect` оставляет гонку sweep ↔ resume закрытой сериализацией на `room.mu`: проигравший resume получает `NotReserved` из `Reconnect`, как и раньше.
