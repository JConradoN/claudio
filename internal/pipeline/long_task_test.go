package pipeline

import "testing"

func TestLooksLikeLongTask(t *testing.T) {
	tests := []struct {
		name string
		text string
		cwd  bool
		want bool
	}{
		{
			name: "empty text",
			text: "",
			cwd:  true,
			want: false,
		},
		{
			name: "no cwd returns false",
			text: "implementa login do usuario",
			cwd:  false,
			want: false,
		},
		{
			name: "simple question without cwd",
			text: "qual e a capital do brasil",
			cwd:  false,
			want: false,
		},
		{
			name: "simple question with cwd",
			text: "qual e a capital do brasil",
			cwd:  true,
			want: false,
		},
		{
			name: "coding verb without multi-step",
			text: "implementa login do usuario",
			cwd:  true,
			want: false,
		},
		{
			name: "coding verb with multi-step marker",
			text: "implementa o login do usuario e depois valida",
			cwd:  true,
			want: true,
		},
		{
			name: "corrigir com lista",
			text: "corrige os bugs no arquivo main.go e tambem no utils.go",
			cwd:  true,
			want: true,
		},
		{
			name: "refatorar com multiplas etapas",
			text: "refatora o modulo de autenticacao: primeiro separa o controller, depois extrai o servico, terceiro adiciona testes",
			cwd:  true,
			want: true,
		},
		{
			name: "english long task",
			text: "implement the login feature and then add validation",
			cwd:  true,
			want: true,
		},
		{
			name: "english short task",
			text: "add login button",
			cwd:  true,
			want: false,
		},
		{
			name: "multi-step without verb",
			text: "o que voce acha sobre isso e tambem sobre aquilo",
			cwd:  true,
			want: false,
		},
		{
			name: "implementar sem cwd",
			text: "implementa login e depois valida",
			cwd:  false,
			want: false,
		},
		{
			name: "investigar com etapas",
			text: "investiga o erro de performance, valida cada endpoint e depois corrige",
			cwd:  true,
			want: true,
		},
		{
			name: "ativa demo sem multi-step",
			text: "ativa o demo do projeto",
			cwd:  true,
			want: false,
		},
		{
			name: "ativa demo com multi-step",
			text: "ativa o demo e depois testa",
			cwd:  true,
			want: true,
		},
		{
			name: "continue sem multi-step marker",
			text: "continue com a implementacao do modulo",
			cwd:  true,
			want: false,
		},
		{
			name: "siga com etapas",
			text: "siga o plano e implemente cada passo",
			cwd:  true,
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := looksLikeLongTask(tc.text, tc.cwd)
			if got != tc.want {
				t.Errorf("looksLikeLongTask(%q, %v) = %v, want %v", tc.text, tc.cwd, got, tc.want)
			}
		})
	}
}
