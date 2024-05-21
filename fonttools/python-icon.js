const path = require("path");
const fs = require("fs");
const { pythonDir, codepointsFile, outputFile } = require("./config.js");

const vueFiles = [];
// Recursively search for all. vue files
function findVueFiles(directory) {
  const files = fs.readdirSync(directory);
  files.forEach((file) => {
    const fullPath = path.join(directory, file);
    if (fs.lstatSync(fullPath).isDirectory()) {
      findVueFiles(fullPath);
    } else if (path.extname(fullPath) === ".vue") {
      vueFiles.push(fullPath);
    }
  });
}
// The function for finding icons can be extended according to specific situations
function findIcons() {
  let iconFiles = [];
  vueFiles.forEach((vueFile) => {
    const content = fs.readFileSync(vueFile, "utf-8");
    if (/sym_r_[^\s'"]*/g.test(content)) {
      const match = content.match(/sym_r_[^\s'"]*/g).map((item) => {
        return item.slice(6);
      });
      iconFiles = [...match, ...iconFiles];
    }
  });
  return iconFiles;
}
findVueFiles(pythonDir);
const icons = findIcons();
const codepointsFilePath = path.join(__dirname, codepointsFile);
// Read file content
fs.readFile(codepointsFilePath, "utf8", (err, data) => {
  if (err) {
    console.error("Error reading file:", err);
    return;
  }
  // Splitting file contents into row arrays
  const lines = data.split(/\r?\n/);
  const outputArr = ["5f-7a", "30-39"];
  // Traverse each row and parse Unicode code points
  lines.forEach((line) => {
    const lineArr = line.split(" ");
    if (icons.find((icon) => icon === lineArr[0])) {
      outputArr.push(lineArr[1]);
    }
  });
  fs.writeFileSync(outputFile, outputArr.join("\n"));
});
