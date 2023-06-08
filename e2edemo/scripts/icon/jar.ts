import fs from 'fs';
import path from 'path';

export class Jar {
  public static readFromFile(base: string | undefined, project: string, version?: string) {
    if (!base) {
      base = "../javascore";
    }
    const build = "build/libs";
    const name = project.replace("/", "-");
    const regex = new RegExp(`${name}-(\\S)+-optimized.jar`, 'g');
    const dir = path.join(base, project, build);
    const files = fs.readdirSync(dir);
    const matchingFiles = files.filter((file) => regex.test(file));
    const optJar = matchingFiles[0];
    const fullPath = path.join(base, project, build, optJar);
    return fs.readFileSync(fullPath).toString('hex')
  }
}
