// Copyright 2023 bytetrade
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tapr

type Middleware struct {
	Postgres   *PostgresConfig   `yaml:"postgres,omitempty"`
	Redis      *RedisConfig      `yaml:"redis,omitempty"`
	MongoDB    *MongodbConfig    `yaml:"mongodb,omitempty"`
	ZincSearch *ZincSearchConfig `yaml:"zincSearch,omitempty"`
}

type Database struct {
	Name        string `json:"name"`
	Distributed bool   `json:"distributed,omitempty"`
}

type PostgresConfig struct {
	Username  string     `yaml:"username" json:"username"`
	Password  string     `yaml:"password,omitempty" json:"password"`
	Databases []Database `yaml:"databases" json:"databases"`
}

type RedisConfig struct {
	Username  string     `yaml:"username" json:"username"`
	Password  string     `yaml:"password,omitempty" json:"password"`
	Databases []Database `yaml:"databases" json:"databases"`
}

type MongodbConfig struct {
	Username  string     `yaml:"username" json:"username"`
	Password  string     `yaml:"password,omitempty" json:"password"`
	Databases []Database `yaml:"databases" json:"databases"`
}

type ZincSearchConfig struct {
	Username string  `yaml:"username" json:"username"`
	Password string  `yaml:"password" json:"password"`
	Indexes  []Index `yaml:"indexes" json:"indexes"`
}

type Index struct {
	Name string `yaml:"name" json:"name"`
}
