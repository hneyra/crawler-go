import prettier from 'eslint-config-prettier';
import path from 'node:path';
import js from '@eslint/js';
import svelte from 'eslint-plugin-svelte';
import { defineConfig, includeIgnoreFile } from 'eslint/config';
import globals from 'globals';
import ts from 'typescript-eslint';

const gitignorePath = path.resolve(import.meta.dirname, '.gitignore');

export default defineConfig(
  includeIgnoreFile(gitignorePath),
  { ignores: ['src/lib/api/schema.d.ts'] },
  js.configs.recommended,
  ts.configs.recommended,
  svelte.configs.recommended,
  prettier,
  svelte.configs.prettier,
  {
    languageOptions: { globals: { ...globals.browser, ...globals.node } },
    rules: {
      // ts handles undefined names.
      'no-undef': 'off'
    }
  },
  {
    files: ['**/*.svelte', '**/*.svelte.ts', '**/*.svelte.js'],
    languageOptions: {
      parserOptions: {
        projectService: true,
        extraFileExtensions: ['.svelte'],
        parser: ts.parser
      }
    }
  },
  {
    rules: {
      // public pages use plain <a href> on purpose (ssr, seo). resolve() adds nothing w/o base path.
      'svelte/no-navigation-without-resolve': 'off',
      // URLSearchParams only used as local scratch to build hrefs, never held as state.
      'svelte/prefer-svelte-reactivity': 'off'
    }
  }
);
