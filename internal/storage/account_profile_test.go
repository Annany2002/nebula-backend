package storage

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountPhotoLifecycleAndPersistentSkip(t *testing.T) {
	db := githubStoreFixture(t)
	ctx := context.Background()
	user, err := FindUserByUserId(ctx, db, "owner")
	require.NoError(t, err)
	require.False(t, user.OnboardingCompleted)
	require.Empty(t, user.AvatarURL)
	require.NoError(t, CompleteOnboarding(ctx, db, "owner"))
	require.NoError(t, CompleteOnboarding(ctx, db, "owner"))
	require.NoError(t, SaveAvatar(ctx, db, "owner", []byte("photo-one")))
	first, err := FindUserByUserId(ctx, db, "owner")
	require.NoError(t, err)
	require.True(t, first.OnboardingCompleted)
	require.Equal(t, "upload", first.AvatarSource)
	id := strings.TrimPrefix(first.AvatarURL, "/avatars/")
	content, err := ReadAvatar(ctx, db, id)
	require.NoError(t, err)
	require.Equal(t, []byte("photo-one"), content)
	require.NoError(t, SaveAvatar(ctx, db, "owner", []byte("photo-two")))
	_, err = ReadAvatar(ctx, db, id)
	require.ErrorIs(t, err, ErrUserNotFound)
	second, err := FindUserByUserId(ctx, db, "owner")
	require.NoError(t, err)
	require.NoError(t, SaveAvatar(ctx, db, "owner", []byte("photo-two")))
	repeated, err := FindUserByUserId(ctx, db, "owner")
	require.NoError(t, err)
	require.Equal(t, second.AvatarURL, repeated.AvatarURL, "identical uploads keep the same asset")
	require.NoError(t, RemoveAvatar(ctx, db, "owner"))
	require.NoError(t, RemoveAvatar(ctx, db, "owner"))
	user, err = FindUserByUserId(ctx, db, "owner")
	require.NoError(t, err)
	require.Empty(t, user.AvatarURL)
	require.Equal(t, "none", user.AvatarSource)
	require.True(t, user.OnboardingCompleted)
	require.NoError(t, SaveAvatar(ctx, db, "owner", []byte("photo-three")))
	user, err = FindUserByUserId(ctx, db, "owner")
	require.NoError(t, err)
	_, err = db.Exec(`DELETE FROM users WHERE user_id='owner'`)
	require.NoError(t, err)
	_, err = ReadAvatar(ctx, db, strings.TrimPrefix(user.AvatarURL, "/avatars/"))
	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestGitHubAvatarDefaultsRespectExplicitPhotoChoices(t *testing.T) {
	db := githubStoreFixture(t)
	ctx := context.Background()
	resolve := func(photo string) string {
		user, err := ResolveGitHubAccount(ctx, db, "123", "octocat", "verified@example.test", false, GitHubLinkIntent{}, photo)
		require.NoError(t, err)
		return user.AvatarURL
	}
	first := "https://avatars.githubusercontent.com/u/123?v=4"
	require.Equal(t, first, resolve(first))
	user, err := FindUserByEmail(ctx, db, "verified@example.test")
	require.NoError(t, err)
	require.True(t, user.OnboardingCompleted)
	require.Equal(t, "github", user.AvatarSource)
	updated := first + "&s=256"
	require.Equal(t, updated, resolve(updated))
	require.NoError(t, SaveAvatar(ctx, db, user.UserId, []byte("custom-photo")))
	require.Contains(t, resolve(first), "/avatars/")
	require.NoError(t, RemoveAvatar(ctx, db, user.UserId))
	require.Empty(t, resolve(first))
	require.Empty(t, resolve("https://evil.test/image"))
}
