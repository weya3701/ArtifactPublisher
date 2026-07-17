package credential

type PersonalAccessToken struct {
	User  string
	Token string
}

func (PersonalAccessToken) Kind() string { return "pat" }
func (c PersonalAccessToken) Username() string {
	if c.User == "" {
		return "AzureDevOps"
	}
	return c.User
}
func (c PersonalAccessToken) Secret() string { return c.Token }

type Basic struct {
	User     string
	Password string
}

func (Basic) Kind() string       { return "basic" }
func (c Basic) Username() string { return c.User }
func (c Basic) Secret() string   { return c.Password }
