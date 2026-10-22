import Js from '@eslint/js'
import Globals from 'globals'
import TsEslint from 'typescript-eslint'
import ReactHooks from 'eslint-plugin-react-hooks'
import ReactRefresh from 'eslint-plugin-react-refresh'

export default TsEslint.config(
  { ignores: ['node_modules/**', 'dist/**'] },
  Js.configs.recommended,
  ...TsEslint.configs.recommended,
  {
    files: ['*.js', 'scripts/*.mjs', 'tests/*.mjs'],
    languageOptions: { globals: Globals.node },
  },
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: { globals: Globals.browser },
    plugins: { 'react-hooks': ReactHooks, 'react-refresh': ReactRefresh },
    rules: {
      ...ReactHooks.configs.recommended.rules,
      'react-refresh/only-export-components': ['error', { allowConstantExport: true }],
    },
  },
)
