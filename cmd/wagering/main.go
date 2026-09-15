package main

import (
	"github.com/leosanner/desafio-jungle-go/internal/composition"

	"go.uber.org/fx"
)

func main() {
	fx.New(composition.Modules()).Run()
}
