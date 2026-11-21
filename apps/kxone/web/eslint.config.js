import js from '@eslint/js'
import globals from 'globals'
import pluginVue from 'eslint-plugin-vue'

export default [
  { ignores: ['dist/**', 'node_modules/**', 'playwright-report/**', 'audit-report/**', 'audit-results/**', 'regression-results/**', 'test-results/**'] },
  js.configs.recommended,
  ...pluginVue.configs['flat/recommended'],
  {
    files: ['**/*.{js,mjs,vue}'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: {
        ...globals.browser,
        ...globals.node,
        wx: 'readonly'
      }
    },
    rules: {
      'vue/html-indent': 'off',
	  'vue/attributes-order': 'off',
	  'vue/html-self-closing': 'off',
      'vue/max-attributes-per-line': 'off',
	  'vue/multiline-html-element-content-newline': 'off',
      'vue/multi-word-component-names': 'off',
      'vue/singleline-html-element-content-newline': 'off'
    }
  },
  {
    files: ['**/*.test.js'],
    languageOptions: { globals: globals.vitest }
  }
]
