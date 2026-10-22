package Auth

import (
	Context "context"
	Cipher "crypto/cipher"
	Subtle "crypto/subtle"
	Errors "errors"
	Strings "strings"
	Time "time"
)

type Service struct {
	Repo          *Repository
	Settings      Options
	Cipher        Cipher.AEAD
	Now           func() Time.Time
	DummyHash     string
	PasswordSlots chan struct{}
}

func NewService(Repo *Repository, Settings Options) (*Service, error) {
	if Err := Settings.Validate(); Err != nil {
		return nil, Err
	}
	AEAD, Err := NewCipher(Settings.EncryptionKey)
	if Err != nil {
		return nil, Err
	}
	Dummy, Err := HashPassword("unused dummy credential")
	if Err != nil {
		return nil, Err
	}
	return &Service{Repo: Repo, Settings: Settings, Cipher: AEAD, Now: Time.Now, DummyHash: Dummy, PasswordSlots: make(chan struct{}, 4)}, nil
}

func (Server *Service) Login(Ctx Context.Context, Username, Password, PreviousIdentifier string) (Authenticated, error) {
	select {
	case Server.PasswordSlots <- struct{}{}:
		defer func() { <-Server.PasswordSlots }()
	default:
		return Authenticated{}, ErrBusy
	}
	Username = Strings.TrimSpace(Username)
	if !UsernamePattern.MatchString(Username) || len(Password) > 1024 {
		return Authenticated{}, ErrCredentials
	}
	Current, Err := Server.Repo.FindUser(Ctx, Username)
	if Err != nil && !Errors.Is(Err, ErrUnauthorized) {
		return Authenticated{}, Err
	}
	Hash := Server.DummyHash
	if Err == nil && Current.IsActive {
		Hash = Current.PasswordHash
	}
	Matches := VerifyPassword(Password, Hash)
	if Err != nil || !Current.IsActive || !Matches {
		return Authenticated{}, ErrCredentials
	}
	if ValidToken(PreviousIdentifier) {
		State, Err := Server.UseSession(Ctx, PreviousIdentifier, true, &Current)
		if Err == nil {
			return State, nil
		}
		if !Errors.Is(Err, ErrUnauthorized) {
			return Authenticated{}, Err
		}
		if Err := Server.Repo.RevokeIdentifier(Ctx, TokenHash(PreviousIdentifier), Server.Now().UTC()); Err != nil {
			return Authenticated{}, Err
		}
	}
	ID, Err := RandomID()
	if Err != nil {
		return Authenticated{}, Err
	}
	Identifier, Err := RandomToken()
	if Err != nil {
		return Authenticated{}, Err
	}
	CSRFToken, Err := RandomToken()
	if Err != nil {
		return Authenticated{}, Err
	}
	Ciphertext, Err := SealSecrets(Server.Cipher, ID, Identifier, CSRFToken)
	if Err != nil {
		return Authenticated{}, Err
	}
	Now := Server.Now().UTC().Truncate(Time.Microsecond)
	Record := Session{ID: ID, UserID: Current.ID, SecretCiphertext: Ciphertext, CreatedAt: Now, LastSeenAt: Now, RotatedAt: Now, AbsoluteExpiresAt: Now.Add(Server.Settings.AbsoluteTimeout)}
	Current, Err = Server.Repo.CreateSession(Ctx, Current, &Record, TokenHash(Identifier))
	if Err != nil {
		return Authenticated{}, Err
	}
	return Authenticated{Principal: Current.Principal, SessionID: ID, Identifier: Identifier, CSRFToken: CSRFToken, AbsoluteExpiresAt: Record.AbsoluteExpiresAt, SetCookie: true}, nil
}

func (Server *Service) Authenticate(Ctx Context.Context, Identifier string) (Authenticated, error) {
	return Server.UseSession(Ctx, Identifier, false, nil)
}

func (Server *Service) UseSession(Ctx Context.Context, Identifier string, ForceRotation bool, Expected *User) (Authenticated, error) {
	if !ValidToken(Identifier) {
		return Authenticated{}, ErrUnauthorized
	}
	var Result Authenticated
	Err := Server.Repo.WithSession(Ctx, TokenHash(Identifier), func(Current User, Record Session) (SessionUpdate, error) {
		Now := Server.Now().UTC().Truncate(Time.Microsecond)
		if Now.Before(Record.LastSeenAt) {
			Now = Record.LastSeenAt
		}
		Update := SessionUpdate{SeenAt: Now, UserAuthVersion: Current.AuthVersion, UserIsPlatformAdmin: Current.IsPlatformAdmin}
		if Expected != nil && Current.ID != Expected.ID {
			return Update, ErrUnauthorized
		}
		if Expected != nil && Current.PasswordHash != Expected.PasswordHash {
			return Update, ErrCredentials
		}
		if Record.RevokedAt != nil {
			return Update, ErrUnauthorized
		}
		if !Current.IsActive || !Now.Before(Record.AbsoluteExpiresAt) || !Now.Before(Record.LastSeenAt.Add(Server.Settings.IdleTimeout)) || (Record.UserIsPlatformAdmin && !Current.IsPlatformAdmin) {
			Update.Revoke = true
			return Update, ErrUnauthorized
		}
		if Record.IdentifierExpiresAt != nil && !Now.Before(*Record.IdentifierExpiresAt) {
			return Update, ErrUnauthorized
		}
		CurrentIdentifier, CSRFToken, Err := OpenSecrets(Server.Cipher, Record.ID, Record.SecretCiphertext)
		if Err != nil {
			Update.Revoke = true
			return Update, ErrUnauthorized
		}
		IsCurrent := Subtle.ConstantTimeCompare([]byte(Identifier), []byte(CurrentIdentifier)) == 1
		Update.Rotate = ForceRotation || Record.UserAuthVersion != Current.AuthVersion || Record.UserIsPlatformAdmin != Current.IsPlatformAdmin || (IsCurrent && !Now.Before(Record.RotatedAt.Add(Server.Settings.RotationInterval)))
		if Update.Rotate {
			CurrentIdentifier, Err = RandomToken()
			if Err != nil {
				return Update, Err
			}
			Update.SecretCiphertext, Err = SealSecrets(Server.Cipher, Record.ID, CurrentIdentifier, CSRFToken)
			if Err != nil {
				return Update, Err
			}
			Update.IdentifierHash = TokenHash(CurrentIdentifier)
			Update.GraceUntil = Now.Add(Server.Settings.GracePeriod)
			if Update.GraceUntil.After(Record.AbsoluteExpiresAt) {
				Update.GraceUntil = Record.AbsoluteExpiresAt
			}
		}
		Result = Authenticated{Principal: Current.Principal, SessionID: Record.ID, Identifier: CurrentIdentifier, CSRFToken: CSRFToken, AbsoluteExpiresAt: Record.AbsoluteExpiresAt, SetCookie: Update.Rotate || !IsCurrent}
		return Update, nil
	})
	if Err != nil {
		return Authenticated{}, Err
	}
	return Result, nil
}
