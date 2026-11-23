// internal/config/consts.go
// Константы сервиса

package config

const (
	// Имя query-параметра (или JSON) для целевого URL хоста.
	ParamTargetUrl = "_rl_url"

	// Имя query-параметра (или JSON) для значения лимита запросов в минуту.
	ParamTargetLimit = "_rl_limit"

	// Имя query-параметра для установленного идентификатора пользователя.
	ParamUserId = "_rl_user_id"
)
