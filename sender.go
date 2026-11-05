package character

// sender.go - delivering sign-in codes.

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
)

// CodeSender delivers a sign-in code to a Discord user.
type CodeSender interface {
	SendCode(ctx context.Context, discordID, code string) error
}

// NewCodeSender picks how codes are delivered: by Discord DM when there is a bot token, or to the
// log when devMode is on. With neither it is an error, so a misconfigured server won't start.
func NewCodeSender(botToken string, devMode bool) (CodeSender, error) {
	switch {
	case botToken != "":
		return NewDiscordSender(botToken)
	case devMode:
		return ConsoleSender{devMode: true}, nil
	}
	return nil, errors.New("no way to send sign-in codes: set a Discord bot token, or turn on dev mode for local development")
}

// DiscordSender sends codes as direct messages. It only makes REST calls, so it needs no gateway
// connection or intents.
type DiscordSender struct {
	session *discordgo.Session
}

func NewDiscordSender(botToken string) (*DiscordSender, error) {
	session, err := discordgo.New("Bot " + botToken)
	if err != nil {
		return nil, fmt.Errorf("create Discord client: %w", err)
	}
	return &DiscordSender{session: session}, nil
}

func (d *DiscordSender) SendCode(ctx context.Context, discordID, code string) error {
	channel, err := d.session.UserChannelCreate(discordID, discordgo.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("open DM channel: %w", err)
	}
	msg := fmt.Sprintf("Your login code is **%s**. It expires in 10 minutes. If you didn't request this, you can ignore it.", code)
	if _, err := d.session.ChannelMessageSend(channel.ID, msg, discordgo.WithContext(ctx)); err != nil {
		return fmt.Errorf("send DM: %w", err)
	}
	return nil
}

// ConsoleSender writes codes to the log, for local development. It only works when it came from
// NewCodeSender with dev mode on; the zero value refuses to send.
type ConsoleSender struct {
	devMode bool
}

func (c ConsoleSender) SendCode(_ context.Context, discordID, code string) error {
	if !c.devMode {
		return errors.New("the console sender is only available in dev mode")
	}
	log.Printf("DEV MODE: sign-in code for Discord user %s is %s", discordID, code)
	return nil
}
