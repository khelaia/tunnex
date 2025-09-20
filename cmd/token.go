package main

import (
	"flag"
	"fmt"
	"os"
	"tunnex/utils"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run cmd/token.go [gen|clear|remove]")
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "gen":
		token, _ := utils.GenerateToken()
		tokens, _ := utils.LoadTokens()
		tokens = append(tokens, token)
		_ = utils.SaveTokens(tokens)
		fmt.Println("New token:", token)

	case "clear":
		_ = utils.SaveTokens([]string{})
		fmt.Println("All tokens cleared.")

	case "remove":
		fs := flag.NewFlagSet("remove", flag.ExitOnError)
		token := fs.String("t", "", "token to remove")
		err := fs.Parse(os.Args[2:])
		if err != nil {
			return
		}
		if *token == "" {
			fmt.Println("Please specify -t <token>")
			os.Exit(1)
		}
		tokens, _ := utils.LoadTokens()
		newTokens := []string{}
		for _, t := range tokens {
			if t != *token {
				newTokens = append(newTokens, t)
			}
		}
		_ = utils.SaveTokens(newTokens)
		fmt.Println("Removed token:", *token)

	default:
		fmt.Println("Unknown command:", cmd)
	}
}
