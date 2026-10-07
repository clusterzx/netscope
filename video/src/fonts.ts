import { loadFont } from '@remotion/fonts';
import interLatin from '@fontsource-variable/inter/files/inter-latin-wght-normal.woff2';
import interLatinExt from '@fontsource-variable/inter/files/inter-latin-ext-wght-normal.woff2';
import mono400 from '@fontsource/jetbrains-mono/files/jetbrains-mono-latin-400-normal.woff2';
import mono500 from '@fontsource/jetbrains-mono/files/jetbrains-mono-latin-500-normal.woff2';
import mono700 from '@fontsource/jetbrains-mono/files/jetbrains-mono-latin-700-normal.woff2';

// Inter (variable) for text, JetBrains Mono for addresses, ports and code
const latin =
	'U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, U+2215, U+FEFF, U+FFFD';
const latinExt =
	'U+0100-02BA, U+02BD-02C5, U+02C7-02CC, U+02CE-02D7, U+02DD-02FF, U+1D00-1DBF, U+1E00-1E9F, U+1EF2-1EFF, U+2020, U+20A0-20AB, U+20AD-20C0, U+2113, U+2C60-2C7F, U+A720-A7FF';

export const fontsReady = Promise.all([
	loadFont({ family: 'Inter', url: interLatin, weight: '100 900', unicodeRange: latin }),
	loadFont({ family: 'Inter', url: interLatinExt, weight: '100 900', unicodeRange: latinExt }),
	loadFont({ family: 'JetBrains Mono', url: mono400, weight: '400' }),
	loadFont({ family: 'JetBrains Mono', url: mono500, weight: '500' }),
	loadFont({ family: 'JetBrains Mono', url: mono700, weight: '700' })
]);
