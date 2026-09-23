import fs from 'node:fs';
import {fileURLToPath} from 'node:url';
import {language as typescript} from '@twinkleplop/typescript';
import {language as htmlLanguage} from '@twinkleplop/html';
import {language as markdown} from '@twinkleplop/markdown';
import {chromium} from 'playwright-core';
const [input,output]=process.argv.slice(2);if(!input||!output)throw Error('Usage: input.html output.html');
const css=fs.readFileSync(fileURLToPath(import.meta.resolve('@twinkleplop/theme-github/dark')),'utf8');
const browser=await chromium.launch({executablePath:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',headless:true});
try{
 const page=await browser.newPage({viewport:{width:500,height:1000}});await page.route('http**',r=>r.abort());
 await page.setContent(fs.readFileSync(input,'utf8'));
 const before=await page.locator('pre').evaluateAll(xs=>xs.map(p=>({code:p.textContent,context:p.closest('article')?.textContent||''})));
 const blocks=before.map(({code,context})=>{
  if(/\.swift\b/.test(context))return {code,lang:'swift-plain',markup:'<pre class="twinkleplop" data-language="swift-plain"><code>'+code.replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;')+'</code></pre>'};
  const lang=code.trimStart().startsWith('<!--')?(/README\.md|\.md\b/.test(context)?'markdown':'html'):'typescript';
  const render={typescript:typescript(),html:htmlLanguage(),markdown:markdown()}[lang];return {code,lang,markup:render(code).replace('<pre class="twinkleplop"','<pre data-language="'+lang+'" class="twinkleplop"')};
 });
 await page.evaluate(({blocks,css})=>{
  [...document.querySelectorAll('pre')].forEach((p,i)=>{p.outerHTML=blocks[i].markup});
  document.getElementById('twinkleplop-theme')?.remove();const style=document.createElement('style');style.id='twinkleplop-theme';style.textContent=css+'\npre.twinkleplop{background:#0d1117;color:#e6edf3;white-space:pre-wrap;overflow-wrap:anywhere}pre.twinkleplop .l{white-space:pre-wrap}';document.head.append(style);
  const walker=document.createTreeWalker(document.body,NodeFilter.SHOW_TEXT);let n;while(n=walker.nextNode()){if(!n.parentElement.closest('pre,script,style'))n.textContent=n.textContent.replace(/Shiki/g,'Twinkleplop');}
 },{blocks,css});
 const after=await page.locator('pre').allTextContents();if(JSON.stringify(after)!==JSON.stringify(before.map(x=>x.code)))throw Error('Code text changed');
 const result=await page.evaluate(()=>({blocks:document.querySelectorAll('pre.twinkleplop').length,shiki:document.querySelectorAll('pre.shiki').length,width:innerWidth,bodyWidth:document.body.scrollWidth,external:document.querySelectorAll('[src],link[href]').length}));
 if(result.shiki||result.bodyWidth>result.width||result.external)throw Error(JSON.stringify(result));
 fs.writeFileSync(output,await page.content());await page.screenshot({path:output+'.png'});console.log(JSON.stringify({...result,languages:blocks.map(x=>x.lang),output}));
}finally{await browser.close()}
