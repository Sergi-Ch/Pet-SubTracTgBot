package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Subscription struct {
	Name      string
	Frequency string
	StartDate time.Time
	Price     float64
}

var (
	bot               *tgbotapi.BotAPI
	userSubscriptions = make(map[int64]map[int]Subscription)
	userTempData      = make(map[int64]Subscription)
	userState         = make(map[int64]string)
	lastSubID         = 0
)

func main() {
	var err error
	bot, err = tgbotapi.NewBotAPI("7275468201:AAGj7YiXAPmkzfJqbCn4anao3mnlJNk0Iqg")
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true
	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil {
			handleMessage(update)
		} else if update.CallbackQuery != nil {
			handleCallback(update)
		}
	}
}

func handleMessage(update tgbotapi.Update) {
	userID := update.Message.From.ID
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "")

	switch userState[userID] {
	case "waiting_name":
		userTempData[userID] = Subscription{Name: update.Message.Text}
		userState[userID] = "waiting_frequency"
		msg.Text = "Выберите частоту оплаты:"
		msg.ReplyMarkup = createFrequencyKeyboard()
	case "waiting_price":
		price, err := strconv.ParseFloat(update.Message.Text, 64)
		if err != nil {
			msg.Text = "Некорректная цена. Введите число:"
		} else {
			sub := userTempData[userID]
			sub.Price = price
			userState[userID] = "waiting_date"
			userTempData[userID] = sub
			msg.Text = "Введите дату первого списания (дд.мм.гггг):"
		}
	case "waiting_date":
		date, err := time.Parse("02.01.2006", update.Message.Text)
		if err != nil {
			msg.Text = "Некорректная дата. Введите в формате дд.мм.гггг:"
		} else {
			sub := userTempData[userID]
			sub.StartDate = date
			addSubscription(userID, sub)
			userState[userID] = ""
			msg.Text = "Подписка успешно добавлена!"
			msg.ReplyMarkup = createMainKeyboard()
		}
	default:
		if update.Message.IsCommand() {
			switch update.Message.Command() {
			case "start":
				msg.Text = "Добро пожаловать! Этот бот поможет отслеживать ваши подписки."
				msg.ReplyMarkup = createMainKeyboard()
			case "help":
				msg.Text = "Используйте кнопки меню для управления подписками"
			case "list":
				msg.Text = listSubscriptions(userID)
				msg.ReplyMarkup = createDeleteKeyboard(userID)
			}
		}
	}

	if _, err := bot.Send(msg); err != nil {
		log.Printf("Error sending message: %v", err)
	}
}

func handleCallback(update tgbotapi.Update) {
	userID := update.CallbackQuery.From.ID
	data := update.CallbackQuery.Data
	msg := tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "")

	switch {
	case data == "add_sub":
		userState[userID] = "waiting_name"
		msg.Text = "Введите название подписки:"
	case data == "list_subs":
		msg.Text = listSubscriptions(userID)
		msg.ReplyMarkup = createDeleteKeyboard(userID)
	case data == "main_menu":
		msg.Text = "Главное меню"
		msg.ReplyMarkup = createMainKeyboard()
	case data == "show_stats":
		monthly, yearly := calculateStats(userID)
		msg.Text = fmt.Sprintf("💰 Статистика:\nЕжемесячно: %.2f руб\nЕжегодно: %.2f руб", monthly, yearly)
		msg.ReplyMarkup = createMainKeyboard()
	case data == "next_payment":
		sub, date := findNextPayment(userID)
		if sub.Name == "" {
			msg.Text = "У вас нет активных подписок"
		} else {
			msg.Text = fmt.Sprintf("⏳ Ближайший платеж:\n%s - %.2f руб\nДата: %s",
				sub.Name, sub.Price, date.Format("02.01.2006"))
		}
		msg.ReplyMarkup = createMainKeyboard()
	case data == "set_freq_month" || data == "set_freq_year":
		sub := userTempData[userID]
		if data == "set_freq_month" {
			sub.Frequency = "month"
		} else {
			sub.Frequency = "year"
		}
		userTempData[userID] = sub
		userState[userID] = "waiting_price"
		msg.Text = "Введите стоимость подписки в рублях:"
	case strings.HasPrefix(data, "delete_"):
		subID, err := strconv.Atoi(strings.TrimPrefix(data, "delete_"))
		if err != nil {
			msg.Text = "Ошибка: неверный ID подписки"
			break
		}

		if _, userExists := userSubscriptions[userID]; !userExists {
			msg.Text = "❌ У вас нет подписок"
		} else if _, subExists := userSubscriptions[userID][subID]; !subExists {
			msg.Text = fmt.Sprintf("❌ Подписка #%d не найдена", subID)
		} else {
			delete(userSubscriptions[userID], subID)
			msg.Text = fmt.Sprintf("✅ Подписка #%d удалена", subID)

			if len(userSubscriptions[userID]) == 0 {
				delete(userSubscriptions, userID)
			}
		}
		msg.ReplyMarkup = createMainKeyboard()
	}

	if _, err := bot.Send(msg); err != nil {
		log.Printf("Error sending message: %v", err)
	}

	callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
	if _, err := bot.Request(callback); err != nil {
		log.Printf("Error answering callback: %v", err)
	}
}

func createMainKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Добавить", "add_sub"),
			tgbotapi.NewInlineKeyboardButtonData("📋 Список", "list_subs"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 Статистика", "show_stats"),
			tgbotapi.NewInlineKeyboardButtonData("⏳ Ближайшее", "next_payment"),
		),
	)
}

func createFrequencyKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Ежемесячно", "set_freq_month"),
			tgbotapi.NewInlineKeyboardButtonData("Ежегодно", "set_freq_year"),
		),
	)
}

func createDeleteKeyboard(userID int64) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	if subs, exists := userSubscriptions[userID]; exists {
		for subID, sub := range subs {
			btn := tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("❌ %s (%.2f руб)", sub.Name, sub.Price),
				fmt.Sprintf("delete_%d", subID),
			)
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
		}
	}

	// Добавляем кнопку возврата в главное меню
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("↩️ Назад", "main_menu"),
	))

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func addSubscription(userID int64, sub Subscription) {
	lastSubID++
	if userSubscriptions[userID] == nil {
		userSubscriptions[userID] = make(map[int]Subscription)
	}
	userSubscriptions[userID][lastSubID] = sub
}

func listSubscriptions(userID int64) string {
	if len(userSubscriptions[userID]) == 0 {
		return "У вас пока нет подписок"
	}

	var sb strings.Builder
	sb.WriteString("📋 Ваши подписки:\n\n")
	for _, sub := range userSubscriptions[userID] {
		sb.WriteString(fmt.Sprintf(" %s\n", sub.Name))
		sb.WriteString(fmt.Sprintf("   💰 Цена: %.2f руб/%s\n", sub.Price, frequencyToRussian(sub.Frequency)))
		sb.WriteString(fmt.Sprintf("   🗓 Следующий платёж: %s\n\n",
			nextPaymentDate(sub.StartDate, sub.Frequency).Format("02.01.2006")))
	}
	return sb.String()
}

func frequencyToRussian(freq string) string {
	if freq == "month" {
		return "месяц"
	}
	return "год"
}

func nextPaymentDate(startDate time.Time, frequency string) time.Time {
	now := time.Now()
	next := startDate
	for next.Before(now) {
		if frequency == "month" {
			next = next.AddDate(0, 1, 0)
		} else {
			next = next.AddDate(1, 0, 0)
		}
	}
	return next
}

func calculateStats(userID int64) (monthly, yearly float64) {
	for _, sub := range userSubscriptions[userID] {
		if sub.Frequency == "month" {
			monthly += sub.Price
			yearly += sub.Price * 12
		} else {
			yearly += sub.Price
			monthly += sub.Price / 12
		}
	}
	return
}

func findNextPayment(userID int64) (Subscription, time.Time) {
	var nextSub Subscription
	var nextDate time.Time
	now := time.Now()

	for _, sub := range userSubscriptions[userID] {
		next := sub.StartDate
		for next.Before(now) {
			if sub.Frequency == "month" {
				next = next.AddDate(0, 1, 0)
			} else {
				next = next.AddDate(1, 0, 0)
			}
		}

		if nextDate.IsZero() || next.Before(nextDate) {
			nextDate = next
			nextSub = sub
		}
	}

	return nextSub, nextDate
}
