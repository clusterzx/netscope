import { Config } from '@remotion/cli/config';

Config.setVideoImageFormat('jpeg');
Config.setJpegQuality(92);
Config.setOverwriteOutput(true);
Config.setCodec('h264');
// use an installed Chrome/Chromium instead of downloading one, e.g. in CI
if (process.env.REMOTION_BROWSER_EXECUTABLE)
	Config.setBrowserExecutable(process.env.REMOTION_BROWSER_EXECUTABLE);
