package config

import admissionadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/admission"

// Config — корневая конфигурация процесса; поля — типы из соответствующих adapter-пакетов.
type Config struct {
	Admission admissionadapter.AdmissionConfig
}
