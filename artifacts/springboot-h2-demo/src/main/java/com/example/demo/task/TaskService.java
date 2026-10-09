package com.example.demo.task;

import org.springframework.stereotype.Service;

import java.util.List;

@Service
public class TaskService {
    private final TaskRepository repository;

    public TaskService(TaskRepository repository) {
        this.repository = repository;
    }

    public List<Task> findAll() {
        return repository.findAll();
    }

    public Task findById(Long id) {
        return repository.findById(id)
                .orElseThrow(() -> new TaskNotFoundException(id));
    }

    public Task create(Task task) {
        task.setCompleted(false);
        return repository.save(task);
    }

    public Task update(Long id, Task input) {
        Task task = findById(id);
        task.setTitle(input.getTitle());
        task.setCompleted(input.isCompleted());
        return repository.save(task);
    }

    public void delete(Long id) {
        repository.delete(findById(id));
    }
}
