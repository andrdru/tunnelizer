package cli

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
)

const pickerTitle = "Tunnel"

// pickAlias спрашивает алиас в терминале. ErrNoAlias означает, что спросить нельзя: список пуст
// или ввод-вывод не терминал, — вызывающий сам решает, что вернуть в этом случае.
func pickAlias(aliases []string) (string, error) {
	if len(aliases) == 0 || !interactiveTerminal() {
		return "", ErrNoAlias
	}

	var chosen string

	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(pickerTitle).
			Options(huh.NewOptions(aliases...)...).
			Filtering(true).
			Value(&chosen),
	)).WithKeyMap(formKeyMap())

	if err := form.Run(); err != nil {
		return "", fmt.Errorf("cli.pickAlias: %w", err)
	}

	return chosen, nil
}

func interactiveTerminal() bool {
	return isTerminal(os.Stdin) && isTerminal(os.Stdout)
}

// isTerminal отличает терминал от файла и пайпа. /dev/null тоже символьное устройство, поэтому
// одной проверки на ModeCharDevice мало: без её исключения форма открывается в скрипте и в тестах.
func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}

	if info.Mode()&os.ModeCharDevice == 0 {
		return false
	}

	devNull, err := os.Stat(os.DevNull)
	if err != nil {
		return false
	}

	return !os.SameFile(info, devNull)
}
