import { chromium } from 'playwright';

async function testPages() {
  console.log('Launching headed Chromium against PRODUCTION daemon...');
  const browser = await chromium.launch({ headless: false, slowMo: 500 });
  const page = await browser.newPage();
  
  // Listen for console errors
  page.on('console', msg => {
    if (msg.type() === 'error') {
      console.log('❌ BROWSER CONSOLE ERROR:', msg.text());
    }
  });
  
  page.on('pageerror', err => {
    console.log('❌ PAGE ERROR:', err.message);
    console.log('Stack:', err.stack);
  });

  // Navigate to production daemon
  console.log('Navigating to http://127.0.0.1:8099...');
  await page.goto('http://127.0.0.1:8099', { waitUntil: 'networkidle' });
  await page.waitForTimeout(5000);
  console.log('✓ Dashboard loaded');

  // Navigate to ModelFit page
  console.log('Looking for ModelFit navigation...');
  try {
    const modelFitLink = page.getByText('Model Fit', { exact: false }).first();
    await modelFitLink.waitFor({ timeout: 3000 });
    await modelFitLink.click();
    await page.waitForTimeout(5000);
    console.log('✓ Navigated to ModelFit page via link');
  } catch {
    console.log('Trying direct navigation to /modelfit...');
    await page.goto('http://127.0.0.1:8099/modelfit', { waitUntil: 'networkidle' });
    await page.waitForTimeout(5000);
    console.log('✓ ModelFit page loaded directly');
  }

  // Check for "something broke" or error boundary
  const modelFitError = await page.getByText('Something broke').count();
  if (modelFitError > 0) {
    const errorText = await page.getByText('Something broke').first().textContent();
    console.log('❌ ModelFit ERROR BOUNDARY:', errorText);
  } else {
    console.log('✅ ModelFit: No error boundary visible');
  }

  // Get page content for ModelFit
  const modelFitContent = await page.textContent('body');
  if (modelFitContent.includes('Something broke')) {
    console.log('❌ ModelFit page contains "Something broke"');
  } else if (modelFitContent.includes('Model Fit') || modelFitContent.includes('ModelFit') || modelFitContent.includes('model') || modelFitContent.includes('Model')) {
    console.log('✅ ModelFit page rendered with content');
  }
  await page.waitForTimeout(5000);

  // Navigate to Licenses page
  console.log('Looking for Licenses navigation...');
  try {
    const licensesLink = page.getByText('Licenses', { exact: false }).first();
    await licensesLink.waitFor({ timeout: 3000 });
    await licensesLink.click();
    await page.waitForTimeout(5000);
    console.log('✓ Navigated to Licenses page via link');
  } catch {
    console.log('Trying direct navigation to /licenses...');
    await page.goto('http://127.0.0.1:8099/licenses', { waitUntil: 'networkidle' });
    await page.waitForTimeout(5000);
    console.log('✓ Licenses page loaded directly');
  }

  // Check for "something broke" or error boundary
  const licensesError = await page.getByText('Something broke').count();
  if (licensesError > 0) {
    const errorText = await page.getByText('Something broke').first().textContent();
    console.log('❌ Licenses ERROR BOUNDARY:', errorText);
  } else {
    console.log('✅ Licenses: No error boundary visible');
  }

  // Get page content for Licenses
  const licensesContent = await page.textContent('body');
  if (licensesContent.includes('Something broke')) {
    console.log('❌ Licenses page contains "Something broke"');
  } else if (licensesContent.includes('License') || licensesContent.includes('license')) {
    console.log('✅ Licenses page rendered with content');
  }
  await page.waitForTimeout(5000);

  await browser.close();
  console.log('Done.');
}

testPages().catch(err => {
  console.error('Test failed:', err);
  process.exit(1);
});