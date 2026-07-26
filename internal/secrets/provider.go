package secrets

type Provider interface {
	Name() string

	Set(appName, key, value string) error

	Get(appName, key string) (string, bool, error)

	Unset(appName, key string) error

	List(appName string) (map[string]string, error)
}
