package user

type RegisterRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Nickname string `json:"nickname"`
}

type PublicUser struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
}

func ToPublicUser(user *User) *PublicUser {
	if user == nil {
		return nil
	}
	return &PublicUser{
		ID:       user.ID,
		Username: user.Username,
		Nickname: user.Nickname,
	}
}
