const fs = require('fs');
const path = require('path');

const storyboardPath = path.join(
  __dirname,
  '..',
  'ios',
  'OnyxSafariPOC',
  'SplashScreen.storyboard',
);

if (!fs.existsSync(storyboardPath)) {
  process.exit(0);
}

let storyboard = fs.readFileSync(storyboardPath, 'utf8');
const originalStoryboard = storyboard;

if (!storyboard.includes('<subviews')) {
  storyboard = storyboard.replace(
    /(\s*)<viewLayoutGuide key="safeArea"/,
    '$1<subviews/>$1<viewLayoutGuide key="safeArea"',
  );
}

if (!storyboard.includes('<constraints')) {
  storyboard = storyboard.replace(
    /(\s*)<color key="backgroundColor"/,
    '$1<constraints/>$1<color key="backgroundColor"',
  );
}

if (storyboard !== originalStoryboard) {
  fs.writeFileSync(storyboardPath, storyboard);
}
