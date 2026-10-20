// Package config читает и печатает .tracker/config.toml.
package config

import "github.com/BurntSushi/toml"

// Зона A заменяет заглушку настоящим разбором TOML.
var _ toml.MetaData

// Column - колонка с ролью.
type Column struct{ Name, Role string }

// Config - содержимое config.toml: колонки и папка=проект.
type Config struct {
	Columns  []Column
	Projects map[string]string
}

// Default возвращает конфиг для первой записи.
func Default() *Config {
	panic("not implemented")
}

// Load читает файл. Нет файла - Default. Ошибка несёт путь и строку.
func Load(path string) (*Config, error) {
	panic("not implemented")
}

// Render печатает config.toml.
func (c *Config) Render() []byte {
	panic("not implemented")
}
