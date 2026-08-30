APP := cms
GO := go
BASHRC ?= $(HOME)/.bashrc
CMS_BASH_COMPLETION_BEGIN := \# cms bash completion
CMS_BASH_COMPLETION_END := \# end cms bash completion

.PHONY: all build test check clean install-bash-completion uninstall-bash-completion

all: build

build:
	$(GO) build -o $(APP) ./cmd/cms

test:
	$(GO) test ./...

check: test build

clean:
	rm -f $(APP)

install-bash-completion: build
	@touch "$(BASHRC)"
	@if grep -Fq "$(CMS_BASH_COMPLETION_BEGIN)" "$(BASHRC)" && grep -Fq "$(CMS_BASH_COMPLETION_END)" "$(BASHRC)"; then \
		sed -i '/^# cms bash completion$$/,/^# end cms bash completion$$/d' "$(BASHRC)"; \
	fi
	@{ \
		printf '\n%s\n' "$(CMS_BASH_COMPLETION_BEGIN)"; \
		./$(APP) shell completion bash; \
		printf '%s\n' "$(CMS_BASH_COMPLETION_END)"; \
	} >> "$(BASHRC)"
	@echo "Installed CMS Bash completion in $(BASHRC)"

uninstall-bash-completion:
	@if grep -Fq "$(CMS_BASH_COMPLETION_BEGIN)" "$(BASHRC)"; then \
		sed -i '/^# cms bash completion$$/,/^# end cms bash completion$$/d' "$(BASHRC)"; \
		echo "Removed CMS Bash completion from $(BASHRC)"; \
	else \
		echo "CMS Bash completion is not installed in $(BASHRC)"; \
	fi
