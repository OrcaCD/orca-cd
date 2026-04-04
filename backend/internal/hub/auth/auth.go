package auth

func Init(appSecret, appURL string) error {
	if err := initJWT(appSecret, appURL); err != nil {
		return err
	}

	if err := initPassword(); err != nil {
		return err
	}
	return nil
}
