export class AsyncQueue {
	private queue: (() => Promise<any>)[] = [];
	private isProcessing = false;

	async enqueue(task: () => Promise<any>): Promise<any> {
		return new Promise((resolve, reject) => {
			this.queue.push(async () => {
				try {
					resolve(await task());
				} catch (error) {
					reject(error);
				}
			});

			this.processQueue();
		});
	}

	private async processQueue(): Promise<void> {
		if (!this.isProcessing && this.queue.length > 0) {
			this.isProcessing = true;
			const task = this.queue.shift();
			if (task) {
				await task();
			}
			this.isProcessing = false;
			this.processQueue();
		}
	}
}
