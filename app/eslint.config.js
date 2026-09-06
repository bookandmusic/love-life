// ESLint 扁平配置（阶段5.6 B16）：Vue 3 + TypeScript
import pluginVue from 'eslint-plugin-vue'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  { ignores: ['dist/**', 'node_modules/**', 'src-tauri/**', '*.config.*'] },
  ...tseslint.configs.recommended,
  ...pluginVue.configs['flat/recommended'],
  {
    files: ['**/*.vue'],
    languageOptions: {
      parserOptions: {
        parser: tseslint.parser,
        extraFileExtensions: ['.vue'],
        sourceType: 'module',
      },
    },
  },
  {
    rules: {
      // .vue 组件名无需强制多词（项目沿用既有单/双词文件名）
      'vue/multi-word-component-names': 'off',
      // 组件 props 未声明默认值时允许缺省
      'vue/require-default-prop': 'off',
      // 模板排版风格由 Prettier 风格统一，不在此施加格式门禁（保持 diff 小）
      'vue/max-attributes-per-line': 'off',
      'vue/singleline-html-element-content-newline': 'off',
      'vue/multiline-html-element-content-newline': 'off',
      'vue/html-indent': 'off',
      'vue/html-self-closing': 'off',
      'vue/attributes-order': 'off',
      'vue/first-attribute-linebreak': 'off',
      // 未使用变量：生产门禁不阻断，仅提示（配合下一步清理）
      '@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
      '@typescript-eslint/no-explicit-any': 'warn',
    },
  },
)