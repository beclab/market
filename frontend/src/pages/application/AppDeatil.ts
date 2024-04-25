import { AppStoreInfo } from 'src/constants/constants';

export interface OnUpdateUITask {
	onInit(): void;
	onUpdate(app: AppStoreInfo): void;
}

export class TaskHandler {
	taskList: OnUpdateUITask[] = [];

	addTask(task: OnUpdateUITask): TaskHandler {
		this.taskList.push(task);
		return this;
	}

	doRunning(app: AppStoreInfo): void {
		if (!app) {
			console.log('app not exist, refuse to update');
			return;
		}

		this.taskList.forEach((task) => {
			task.onInit();
			task.onUpdate(app);
		});
	}
}
