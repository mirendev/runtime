package urlx

import (
	"fmt"
	"net/url"
)

func AsMysqlDSN(u string) (string, error) {
	ec, err := url.Parse(u)
	if err != nil {
		return "", err
	}

	if ec.Scheme != "mysql" {
		return "", fmt.Errorf("expected mysql scheme, got %s", ec.Scheme)
	}

	name := ec.User.Username()
	pass, _ := ec.User.Password()

	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s)/%s", name, pass, ec.Host, ec.Path[1:])

	return dsn, nil
}
